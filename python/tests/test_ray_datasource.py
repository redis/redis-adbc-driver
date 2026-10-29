"""Worker-side Ray ingestion, plus deterministic stream lifecycle checks."""

from decimal import Decimal
import os
import uuid

import pyarrow as pa
import pytest

ray = pytest.importorskip("ray")

from adbc_driver_redis import dbapi
from adbc_driver_redis.ray import RedisDatasource, _read_partition, read_query

pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_RAY_TESTS") != "1",
    reason="set RUN_RAY_TESTS=1 to run the optional Ray datasource suite",
)


@pytest.fixture
def db_options():
    ca = os.environ.get("REDIS_TLS_CA_FILE")
    return {"adbc.redis.tls_ca_file": ca} if ca else {}


@pytest.fixture(scope="module")
def ray_runtime():
    ray.init(num_cpus=2, include_dashboard=False, log_to_driver=False,
             object_store_memory=128 * 1024 * 1024)
    try:
        yield
    finally:
        ray.shutdown()


@pytest.fixture
def source(uri, db_options):
    name = f"it_ray_workers_{uuid.uuid4().hex}"
    table = pa.table({
        "id": pa.array(range(73), type=pa.int64()),
        "label": [None if i % 3 == 0 else f"Redis → Ray {i}" for i in range(73)],
        "amount": pa.array([Decimal("1.23"), None] * 36 + [Decimal("-9.87")],
                           type=pa.decimal128(12, 2)),
        "flag": pa.array([True, None] * 36 + [False], type=pa.bool_()),
        "payload": pa.array([b"\x00\xff", None] * 36 + [b""], type=pa.binary()),
    })
    with dbapi.connect(uri, db_kwargs=db_options) as conn, conn.cursor() as cur:
        cur.adbc_ingest(name, table, mode="create")
    try:
        yield name, table
    finally:
        with dbapi.connect(uri, db_kwargs=db_options) as conn, conn.cursor() as cur:
            cur.execute(f'DROP TABLE IF EXISTS "{name}"')


def collect(dataset):
    return pa.concat_tables(ray.get(dataset.to_arrow_refs()))


def test_dataset_types_coverage_and_empty(uri, source, ray_runtime, db_options):
    name, table = source
    dataset = read_query(uri, f'SELECT * FROM "{name}" WHERE id >= ?',
                         parameters=(0,), partition_count=4, concurrency=2,
                         batch_rows=5, db_kwargs=db_options)
    result = collect(dataset).sort_by("id")
    assert result.equals(table, check_metadata=True)
    processed = collect(dataset.map_batches(lambda batch: batch, batch_format="pyarrow"))
    assert processed.sort_by("id").equals(table, check_metadata=True)
    empty = read_query(uri, f'SELECT * FROM "{name}" WHERE false',
                       partition_count=4, batch_rows=5, db_kwargs=db_options)
    assert empty.schema().base_schema.equals(table.schema, check_metadata=True)
    assert empty.take_all() == []
    assert empty.schema().base_schema.equals(table.schema, check_metadata=True)


def test_remote_serialization_streaming_and_live_replay(uri, source, ray_runtime, db_options):
    name, table = source
    # All planner handles have closed before tasks are serialized.
    source_ds = RedisDatasource(uri, f'SELECT * FROM "{name}"',
                               partition_count=4, batch_rows=5, db_kwargs=db_options)
    tasks = source_ds.get_read_tasks(4)

    @ray.remote
    def consume(task):
        blocks = list(task())
        return os.getpid(), [len(b) for b in blocks], pa.concat_tables(blocks)

    results = ray.get([consume.remote(task) for task in tasks])
    assert all(pid != os.getpid() for pid, _, _ in results)
    assert all(n <= 5 for _, sizes, _ in results for n in sizes)
    assert any(len(sizes) > 1 for _, sizes, _ in results)
    actual = pa.concat_tables([t for _, _, t in results]).sort_by("id")
    assert actual.equals(table, check_metadata=True)
    with dbapi.connect(uri, db_kwargs=db_options) as conn, conn.cursor() as cur:
        cur.execute(f'UPDATE "{name}" SET label = \'changed\' WHERE id = 0')
    replay = ray.get([consume.remote(task) for task in tasks])
    assert pa.concat_tables([t for _, _, t in replay]).sort_by("id")["label"][0].as_py() == "changed"


def test_unsupported_query_fails_planning(uri, source, db_options):
    name, _ = source
    with pytest.raises(dbapi.NotSupportedError):
        RedisDatasource(uri, f'SELECT count(*) FROM "{name}"', db_kwargs=db_options)


@pytest.mark.parametrize("kwargs", [
    {"partition_count": 0}, {"partition_count": 1025}, {"batch_rows": True},
    {"partition_scan_cap": -1}, {"concurrency": 0},
])
def test_options_fail_before_connect(monkeypatch, kwargs):
    def unexpected(*args, **options):
        pytest.fail("invalid options opened a connection")
    monkeypatch.setattr(dbapi, "connect", unexpected)
    with pytest.raises(ValueError):
        read_query("redis://unused", "SELECT * FROM events", **kwargs)


@pytest.mark.parametrize("ending", ["complete", "close", "failure", "cancel", "schema"])
def test_reader_lifecycle(monkeypatch, ending):
    schema = pa.schema([("id", pa.int64())])
    closed = []
    fetched = []

    class Handle:
        def __init__(self, name):
            self.name = name
        def __enter__(self):
            return self
        def __exit__(self, *args):
            closed.append(self.name)
        def cursor(self):
            return Handle("cursor")
        def adbc_read_partition(self, descriptor):
            assert descriptor == b"opaque"
        def fetch_record_batch(self):
            return Reader("reader")

    class Reader(Handle):
        @property
        def schema(self):
            return pa.schema([("wrong", pa.int64())]) if ending == "schema" else schema
        def __iter__(self):
            fetched.append(1)
            yield pa.record_batch([[1]], schema=schema)
            fetched.append(2)
            if ending == "failure":
                raise RuntimeError("injected batch failure")
            yield pa.record_batch([[2]], schema=schema)

    monkeypatch.setattr(dbapi, "connect", lambda *a, **k: Handle("connection"))
    stream = _read_partition(b"opaque", uri="redis://unused", schema=schema,
                             db_kwargs={}, conn_kwargs={})
    if ending == "schema":
        with pytest.raises(ValueError, match="schema"):
            next(stream)
    else:
        assert next(stream).to_pylist() == [{"id": 1}]
        assert fetched == [1]  # No read-ahead or whole-partition materialization.
        assert not closed
        if ending == "complete":
            assert pa.concat_tables(list(stream)).to_pylist() == [{"id": 2}]
        elif ending == "failure":
            with pytest.raises(RuntimeError, match="injected batch failure"):
                next(stream)
        elif ending == "cancel":
            with pytest.raises(KeyboardInterrupt):
                stream.throw(KeyboardInterrupt())
        else:
            stream.close()
    assert closed == ["reader", "cursor", "connection"]


def test_remote_early_release_and_retry(uri, source, ray_runtime, db_options):
    name, table = source
    task = RedisDatasource(uri, f'SELECT * FROM "{name}"',
                           partition_count=1, batch_rows=5, db_kwargs=db_options).get_read_tasks(1)[0]

    @ray.remote
    def early_then_retry(task):
        stream = task.read_fn()
        assert next(stream).num_rows <= 5
        stream.close()
        # A new attempt owns a fresh connection and starts the partition again.
        return pa.concat_tables(list(task()))

    assert ray.get(early_then_retry.remote(task)).sort_by("id").equals(table)


def test_remote_native_cancellation(uri, source, ray_runtime, db_options):
    name, _ = source
    task = RedisDatasource(uri, f'SELECT * FROM "{name}"', partition_count=1,
                           batch_rows=1, db_kwargs=db_options).get_read_tasks(1)[0]

    @ray.remote
    def cancel_read(task):
        from unittest.mock import patch
        original = dbapi.connect
        connections = []

        def capture(*args, **kwargs):
            conn = original(*args, **kwargs)
            connections.append(conn)
            return conn

        with patch("adbc_driver_redis.dbapi.connect", side_effect=capture):
            stream = task.read_fn()
            try:
                next(stream)
                connections[0].adbc_cancel()
                try:
                    list(stream)
                except RuntimeError as exc:
                    assert "cancelled" in str(exc)
                else:
                    raise AssertionError("cancelled native stream completed successfully")
            finally:
                stream.close()
        # Reader unwind closes the native DB-API connection.
        assert connections[0]._closed
        return sum(block.num_rows for block in task())

    assert ray.get(cancel_read.remote(task)) == 73


def test_ray_task_retry(uri, source, ray_runtime, db_options):
    name, table = source
    task = RedisDatasource(uri, f'SELECT * FROM "{name}"', partition_count=1,
                           batch_rows=5, db_kwargs=db_options).get_read_tasks(1)[0]

    @ray.remote(num_cpus=0)
    class Attempts:
        def __init__(self):
            self.count = 0
        def next(self):
            self.count += 1
            return self.count

    @ray.remote(max_retries=1, retry_exceptions=[RuntimeError])
    def retry_read(task, attempts):
        attempt = ray.get(attempts.next.remote())
        stream = task.read_fn()
        try:
            first = next(stream)
            if attempt == 1:
                raise RuntimeError("injected failure after first Arrow batch")
            return attempt, pa.concat_tables([first, *stream])
        finally:
            stream.close()

    attempts = Attempts.remote()
    try:
        attempt, result = ray.get(retry_read.remote(task, attempts))
        assert attempt == 2
        assert result.sort_by("id").equals(table, check_metadata=True)
    finally:
        ray.kill(attempts)


def test_tls_worker_requires_ca(uri, source, ray_runtime, db_options):
    if not db_options:
        pytest.skip("private CA fixture required")
    name, _ = source
    task = RedisDatasource(uri, f'SELECT * FROM "{name}"',
                           db_kwargs=db_options).get_read_tasks(1)[0]

    @ray.remote
    def without_ca(task):
        read = task.read_fn
        options = dict(read.keywords)
        options["db_kwargs"] = {"adbc.redis.stream_batch_rows": "5"}
        return list(_read_partition(*read.args, **options))

    with pytest.raises(ray.exceptions.RayTaskError, match="(?i)certificate"):
        ray.get(without_ca.remote(task))
