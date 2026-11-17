"""Worker-local append, partial failure and Dask recomputation contracts."""

import datetime as dt
import os
import pickle
import uuid
from contextlib import contextmanager
from decimal import Decimal

import dask.dataframe as dd
import pandas as pd
import pyarrow as pa
import pytest
from dask import delayed
from distributed import Client, LocalCluster

from adbc_driver_redis import dbapi
from adbc_driver_redis.dask import PartitionWriteError, write_dataframe

SCHEMA = pa.schema([("id", pa.int64()), ("label", pa.string())])
SOURCE = pa.table({"id": [1, None, 3], "label": ["雪", None, "last"]}, schema=SCHEMA)


def frame_from_parts(parts):
    return dd.from_delayed(
        [delayed(lambda value: value, pure=False)(part) for part in parts],
        meta=SCHEMA.empty_table().to_pandas(types_mapper=pd.ArrowDtype),
        verify_meta=False,
    )


def source_frame():
    return frame_from_parts(
        [SOURCE.slice(0, 2).to_pandas(types_mapper=pd.ArrowDtype),
         SCHEMA.empty_table().to_pandas(types_mapper=pd.ArrowDtype),
         SOURCE.slice(2).to_pandas(types_mapper=pd.ArrowDtype)]
    )


@pytest.mark.parametrize("failure", [None, "convert", "ingest", "count", "close"])
def test_lazy_append_counts_and_cleanup(monkeypatch, failure):
    events, written = [], []

    class Cursor:
        def adbc_ingest(self, table, data, *, mode, db_schema_name):
            assert (table, mode, db_schema_name) == ("target", "append", "public")
            index = len(written)
            written.append(data)
            events.append(("write", index, data.num_rows))
            if index == 1 and failure == "ingest":
                raise RuntimeError("lost reply")
            if index == 1 and failure == "count":
                return -1
            return data.num_rows

    class Connection:
        @contextmanager
        def cursor(self):
            try:
                yield Cursor()
            finally:
                events.append("cursor closed")

    @contextmanager
    def connect(uri, *, db_kwargs, conn_kwargs):
        assert (uri, db_kwargs, conn_kwargs) == ("unused", {"x": "y"}, {})
        events.append("connected")
        try:
            yield Connection()
        finally:
            events.append("connection closed")
            if failure == "close" and len(written) == 2:
                raise RuntimeError("close failed")

    monkeypatch.setattr(dbapi, "connect", connect)
    first = SOURCE.slice(0, 2).to_pandas(types_mapper=pd.ArrowDtype)
    second = SOURCE.slice(2).to_pandas(types_mapper=pd.ArrowDtype)
    if failure == "convert":
        second = pd.DataFrame({"id": ["not an integer"], "label": ["bad"]})
    frame = frame_from_parts([first, second])
    job = write_dataframe(
        frame, "unused", "target", schema=SCHEMA, db_schema_name="public",
        allow_duplicate_writes=True, db_kwargs={"x": "y"},
    )
    assert events == []
    layers = job.__dask_graph__().layers
    writes = [layer for name, layer in layers.items() if name.startswith("_append_partition-")]
    assert len(writes) == 2
    assert all(layer.annotations == {"retries": 0} for layer in writes)
    if failure:
        with pytest.raises(PartitionWriteError) as caught:
            job.compute(scheduler="synchronous")
        error = caught.value
        assert error.partition_index == 1
        assert error.acknowledged_rows == 2
        assert error.write_attempted == (failure != "convert")
        assert error.__cause__ is not None
        restored = pickle.loads(pickle.dumps(error))
        assert str(restored) == str(error)
        assert restored.args == error.args
        assert len(written) == (1 if failure == "convert" else 2)
    else:
        assert job.compute(scheduler="synchronous") == 3
        assert pa.concat_tables(written).equals(SOURCE)
        # A second compute really appends again: consent must not imply idempotency.
        assert job.compute(scheduler="synchronous") == 3
        assert pa.concat_tables(written).equals(pa.concat_tables([SOURCE, SOURCE]))
    assert events.count("cursor closed") == events.count("connected")
    assert events.count("connection closed") == events.count("connected")


def test_empty_partition_is_validated_on_target(monkeypatch):
    calls = []

    class Cursor:
        def adbc_ingest(self, table, data, **kwargs):
            calls.append(data)
            return 0

    class Connection:
        @contextmanager
        def cursor(self):
            yield Cursor()

    @contextmanager
    def connect(*args, **kwargs):
        yield Connection()

    monkeypatch.setattr(dbapi, "connect", connect)
    frame = frame_from_parts([SCHEMA.empty_table().to_pandas(types_mapper=pd.ArrowDtype)])
    assert write_dataframe(frame, "unused", "target", schema=SCHEMA,
                           allow_duplicate_writes=True).compute(scheduler="synchronous") == 0
    assert len(calls) == 1
    assert calls[0].schema.equals(SCHEMA)


@pytest.mark.parametrize("overrides,error", [
    ({"allow_duplicate_writes": False}, ValueError),
    ({"allow_duplicate_writes": 1}, ValueError),
    ({"frame": pd.DataFrame()}, TypeError),
    ({"schema": None}, TypeError),
    ({"schema": pa.schema([("id", pa.int64()), ("id", pa.string())])}, ValueError),
    ({"schema": pa.schema([("label", pa.string()), ("id", pa.int64())])}, ValueError),
    ({"table": None}, ValueError),
    ({"table": ""}, ValueError),
    ({"table": "bad" + chr(0)}, ValueError),
    ({"db_schema_name": ""}, ValueError),
])
def test_writer_refuses_invalid_inputs(overrides, error):
    options = dict(frame=source_frame(), uri="unused", table="target", schema=SCHEMA,
                   allow_duplicate_writes=True)
    options.update(overrides)
    with pytest.raises(error):
        write_dataframe(**options)


def test_writer_requires_explicit_replay_consent():
    with pytest.raises(ValueError, match="allow_duplicate_writes=True"):
        write_dataframe(source_frame(), "unused", "target", schema=SCHEMA)


@pytest.mark.parametrize("values,schema", [
    ([128], pa.schema([pa.field("id", pa.int8())])),
    ([None], pa.schema([pa.field("id", pa.int64(), nullable=False)])),
    ([1.25], pa.schema([pa.field("id", pa.int64())])),
])
def test_safe_conversion_refuses_loss_and_nulls(monkeypatch, values, schema):
    def forbidden_connect(*args, **kwargs):
        pytest.fail("conversion refusal must precede opening a connection")

    monkeypatch.setattr(dbapi, "connect", forbidden_connect)
    frame = dd.from_pandas(pd.DataFrame({"id": values}), npartitions=1)
    with pytest.raises(PartitionWriteError) as caught:
        write_dataframe(frame, "unused", "target", schema=schema,
                        allow_duplicate_writes=True).compute(scheduler="synchronous")
    assert caught.value.acknowledged_rows == 0
    assert not caught.value.write_attempted


def _pid():
    return os.getpid()


def test_distributed_arrow_appends_and_failure(uri):
    table = "it_dask_write_" + uuid.uuid4().hex
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.adbc_ingest(table, SCHEMA.empty_table(), mode="create")
        try:
            with LocalCluster(n_workers=2, threads_per_worker=1, processes=True,
                              dashboard_address=None) as cluster, Client(cluster) as client:
                workers = sorted(client.scheduler_info()["workers"])
                pids = client.run(_pid)
                assert len(set(pids.values())) == 2
                assert os.getpid() not in pids.values()
                # Pin consecutive evaluations to different worker processes.
                for worker in workers:
                    job = write_dataframe(source_frame(), uri, table, schema=SCHEMA,
                                          allow_duplicate_writes=True)
                    future = client.compute(job, workers=[worker], allow_other_workers=False)
                    assert future.result() == 3
                    assert client.run_on_scheduler(
                        lambda key, dask_scheduler: dask_scheduler.tasks[key].retries,
                        key=future.key,
                    ) == 0
                cur.execute(f'SELECT id, label FROM "{table}" ORDER BY id NULLS LAST, label')
                expected = pa.concat_tables([SOURCE, SOURCE]).sort_by([("id", "ascending"), ("label", "ascending")])
                assert cur.fetch_arrow_table().equals(expected)

                good = SOURCE.slice(0, 1).to_pandas(types_mapper=pd.ArrowDtype)
                bad = pd.DataFrame({"id": ["not an integer"], "label": ["bad"]})
                never = SOURCE.slice(2).to_pandas(types_mapper=pd.ArrowDtype)
                job = write_dataframe(frame_from_parts([good, bad, never]), uri, table,
                                      schema=SCHEMA, allow_duplicate_writes=True)
                with pytest.raises(PartitionWriteError) as caught:
                    client.compute(job, workers=[workers[0]], allow_other_workers=False).result()
                assert caught.value.partition_index == 1
                assert caught.value.acknowledged_rows == 1
                assert not caught.value.write_attempted
                cur.execute(f'SELECT id, label FROM "{table}" ORDER BY id NULLS LAST, label')
                expected = pa.concat_tables([SOURCE, SOURCE, SOURCE.slice(0, 1)]).sort_by(
                    [("id", "ascending"), ("label", "ascending")])
                assert cur.fetch_arrow_table().equals(expected)
                # A real native append refusal must not consume later partitions or delete prior data.
                missing = "missing_" + uuid.uuid4().hex
                job = write_dataframe(source_frame(), uri, missing, schema=SCHEMA,
                                      allow_duplicate_writes=True)
                with pytest.raises(PartitionWriteError) as caught:
                    client.compute(job, workers=[workers[1]], allow_other_workers=False).result()
                assert caught.value.partition_index == 0
                assert caught.value.acknowledged_rows == 0
                assert caught.value.write_attempted
                cur.execute(f'SELECT COUNT(*) FROM "{table}"')
                assert cur.fetchone()[0] == 7

                typed_name = table + "_typed"
                typed = pa.table({
                    "id": pa.array([2**53 + 1, 2, 3], type=pa.int64()),
                    "amount": pa.array([Decimal("1234567890.12"), None, Decimal("-0.01")],
                                       type=pa.decimal128(12, 2)),
                    "flag": pa.array([True, None, False], type=pa.bool_()),
                    "instant": pa.array([dt.datetime(2024, 2, 29, 1, 2, 3, 456789),
                                         None, dt.datetime(1970, 1, 1)], type=pa.timestamp("us")),
                    "day": pa.array([dt.date(2024, 2, 29), None, dt.date(1970, 1, 1)],
                                    type=pa.date32()),
                    "payload": pa.array([bytes([0, 255]), None, b""], type=pa.binary()),
                })
                assert 0 in typed["payload"][0].as_py()
                cur.adbc_ingest(typed_name, typed.schema.empty_table(), mode="create")
                try:
                    meta = typed.schema.empty_table().to_pandas(types_mapper=pd.ArrowDtype)
                    parts = [delayed(lambda value: value, pure=False)(
                        part.to_pandas(types_mapper=pd.ArrowDtype))
                        for part in [typed.slice(0, 1), typed.slice(0, 0), typed.slice(1)]]
                    frame = dd.from_delayed(parts, meta=meta)
                    job = write_dataframe(frame, uri, typed_name, schema=typed.schema,
                                          allow_duplicate_writes=True)
                    assert client.compute(job, workers=[workers[1]],
                                          allow_other_workers=False).result() == 3
                    with dbapi.connect(uri) as fresh, fresh.cursor() as check:
                        check.execute(f'SELECT * FROM "{typed_name}" ORDER BY id')
                        actual = check.fetch_arrow_table()
                    assert actual.equals(typed.sort_by([("id", "ascending")])), actual.schema
                finally:
                    cur.execute(f'DROP TABLE "{typed_name}"')
        finally:
            cur.execute(f'DROP TABLE "{table}"')
