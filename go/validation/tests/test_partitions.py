"""Portable deferred ADBC partitions through the native C driver boundary."""

import base64
import decimal
import json
import os
import subprocess
import sys
import queue
import threading

import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
import pytest


READ_PARTITION = """
import base64, json, sys
import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
p = json.load(sys.stdin)
with dbapi.connect(driver=p['driver'], db_kwargs=p['db'], autocommit=True) as conn:
    with conn.cursor() as cur:
        cur.adbc_read_partition(base64.b64decode(p['partition']))
        table = cur.fetch_arrow_table()
        sink = pa.BufferOutputStream()
        with pa.ipc.new_stream(sink, table.schema) as writer:
            writer.write_table(table)
        print(base64.b64encode(sink.getvalue()).decode())
"""


def worker_kwargs(db_kwargs):
    return {**db_kwargs, "uri": os.environ.get("REDIS_WORKER_URI") or db_kwargs["uri"]}


def connect(driver_path, db_kwargs):
    return dbapi.connect(driver=driver_path, db_kwargs=db_kwargs, autocommit=True)


def test_partition_planner_exit_and_worker_death(driver_path, db_kwargs):
    table = "it_partition_process_death"
    data = pa.table({"id": pa.array(range(300), pa.int64()),
                     "label": [None if i % 3 == 0 else "exact\x00value" for i in range(300)],
                     "payload": pa.array([b"\x00\xff"] * 300, pa.binary())})
    planner = r'''
import base64, json, os, sys
import adbc_driver_manager.dbapi as dbapi
p = json.load(sys.stdin)
with dbapi.connect(driver=p['driver'], db_kwargs=p['db'], autocommit=True) as conn:
    with conn.cursor() as cur:
        cur.adbc_statement.set_options(**{'adbc.redis.partition_count':1,
            'adbc.redis.partition_scan_cap':p['cap']})
        parts, _ = cur.adbc_execute_partitions('SELECT * FROM it_partition_process_death')
        assert len(parts) == 1
        print(base64.b64encode(parts[0]).decode(), flush=True)
        os._exit(77)  # Bypass statement/connection/destructor cleanup deliberately.
'''
    partial_worker = r'''
import base64, json, sys, threading
import adbc_driver_manager.dbapi as dbapi
p = json.load(sys.stdin)
with dbapi.connect(driver=p['driver'], db_kwargs={**p['db'],
        'adbc.redis.stream_batch_rows':'10'}, autocommit=True) as conn:
    with conn.cursor() as cur:
        cur.adbc_read_partition(base64.b64decode(p['partition']))
        with cur.fetch_record_batch() as reader:
            assert reader.read_next_batch().num_rows == 10
            print('first-batch', flush=True)
            threading.Event().wait()  # Parent kills us with a live partial reader.
'''
    with connect(driver_path, db_kwargs) as conn:
        with conn.cursor() as cur:
            cur.execute(f"DROP TABLE IF EXISTS {table}")
            cur.adbc_ingest(table, data)
    try:
        result = subprocess.run([sys.executable, "-c", planner], text=True,
            input=json.dumps({"driver":driver_path, "db":db_kwargs,
                              "cap":int(os.environ.get("REDIS_PARTITION_SCAN_CAP", "0"))}),
            capture_output=True, timeout=60)
        assert result.returncode == 77, "planner did not exit after publishing its descriptor"
        descriptor = result.stdout.strip()
        payload = json.dumps({"driver":driver_path, "db":worker_kwargs(db_kwargs),
                              "partition":descriptor})
        process = subprocess.Popen([sys.executable, "-c", partial_worker],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        ready = queue.Queue()
        thread = threading.Thread(target=lambda: ready.put(process.stdout.readline()), daemon=True)
        thread.start()
        try:
            process.stdin.write(payload)
            process.stdin.close()
            assert ready.get(timeout=30).strip() == "first-batch"
            process.kill()
            assert process.wait(timeout=10) != 0
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=10)
            thread.join(timeout=10)
            process.stdout.close()
            if not process.stdin.closed:
                process.stdin.close()
        # A failed worker must not invalidate the descriptor. Retry observes live
        # updates/deletes, and excludes the insert beyond its planned watermark.
        with connect(driver_path, db_kwargs) as conn:
            with conn.cursor() as cur:
                cur.execute(f"UPDATE {table} SET label='changed' WHERE id=1")
                cur.execute(f"DELETE FROM {table} WHERE id=3")
                cur.execute(f"INSERT INTO {table} (id) VALUES(300)")
        result = subprocess.run([sys.executable, "-c", READ_PARTITION], input=payload,
            text=True, capture_output=True, timeout=60)
        assert result.returncode == 0, "new worker could not retry after process death"
        actual = pa.ipc.open_stream(base64.b64decode(result.stdout)).read_all().sort_by("id")
        expected = [row for row in data.to_pylist() if row["id"] != 3]
        expected[1]["label"] = "changed"
        assert actual.schema.equals(data.schema)
        assert actual.to_pylist() == expected
    finally:
        with connect(driver_path, db_kwargs) as conn:
            with conn.cursor() as cur:
                cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_partitions_cross_process(driver_path, db_kwargs):
    table = "it_partition_python"
    data = pa.table({
        "id": pa.array([1, 2, 3, 4, 5], pa.int64()),
        "label": ["a", None, "c", "d", "e"],
        "amount": pa.array([decimal.Decimal("1.23"), None, decimal.Decimal("3.45"),
                            decimal.Decimal("4.56"), decimal.Decimal("5.67")], pa.decimal128(12, 2)),
        "flag": [True, False, None, True, False],
    })
    with connect(driver_path, db_kwargs) as conn:
        with conn.cursor() as cur:
            if os.environ.get("REDIS_PARTITION_SCAN_CAP"):
                cur.adbc_statement.set_options(**{"adbc.redis.partition_scan_cap": int(os.environ["REDIS_PARTITION_SCAN_CAP"])})
            cur.execute(f"DROP TABLE IF EXISTS {table}")
            cur.adbc_ingest(table, data)
            cur.execute(f"DELETE FROM {table} WHERE id=3")
            cur.execute(f"SELECT * FROM {table}")
            expected = cur.fetch_arrow_table().sort_by("id")
            cur.adbc_statement.set_options(**{"adbc.redis.partition_count": 4,
                "adbc.redis.partition_scan_cap": int(os.environ.get("REDIS_PARTITION_SCAN_CAP", "0"))})
            parts, schema = cur.adbc_execute_partitions(f"SELECT * FROM {table}")
            assert cur.rowcount == -1
            assert len(parts) == 4
    # Both planning handles have closed before any descriptor is consumed.
    try:
        tables = []
        for part in parts:
            result = subprocess.run(
                [sys.executable, "-c", READ_PARTITION],
                input=json.dumps({"driver": driver_path, "db": worker_kwargs(db_kwargs),
                                  "partition": base64.b64encode(part).decode()}),
                text=True, capture_output=True, check=True, timeout=60,
            )
            tables.append(pa.ipc.open_stream(base64.b64decode(result.stdout)).read_all())
        actual = pa.concat_tables(tables).sort_by("id")
        assert actual.schema.equals(schema)
        assert actual.equals(expected)
        with connect(driver_path, db_kwargs) as conn:
            with conn.cursor() as cur:
                if os.environ.get("REDIS_PARTITION_SCAN_CAP"):
                    cur.adbc_statement.set_options(**{"adbc.redis.partition_scan_cap": int(os.environ["REDIS_PARTITION_SCAN_CAP"])})
                cur.adbc_read_partition(parts[0])
                retry = cur.fetch_arrow_table()
                assert retry.equals(tables[0])
    finally:
        with connect(driver_path, db_kwargs) as conn:
            with conn.cursor() as cur:
                if os.environ.get("REDIS_PARTITION_SCAN_CAP"):
                    cur.adbc_statement.set_options(**{"adbc.redis.partition_scan_cap": int(os.environ["REDIS_PARTITION_SCAN_CAP"])})
                cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_partition_validation_and_empty(driver_path, db_kwargs):
    with connect(driver_path, db_kwargs) as conn:
        with conn.cursor() as cur:
            if os.environ.get("REDIS_PARTITION_SCAN_CAP"):
                cur.adbc_statement.set_options(**{"adbc.redis.partition_scan_cap": int(os.environ["REDIS_PARTITION_SCAN_CAP"])})
            cur.execute("DROP TABLE IF EXISTS it_partition_python_empty")
            cur.execute("CREATE TABLE it_partition_python_empty (id BIGINT)")
            try:
                for _ in range(20):  # repeated schema/descriptor export and release
                    parts, schema = cur.adbc_execute_partitions("SELECT * FROM it_partition_python_empty")
                    assert len(parts) == 1
                    cur.adbc_read_partition(parts[0])
                    empty = cur.fetch_arrow_table()
                    assert empty.num_rows == 0
                    assert empty.schema.equals(schema)
                for bad in (b"", b"{}", b"null", b"not-json", parts[0] + b"{}"):
                    with pytest.raises(dbapi.ProgrammingError):
                        cur.adbc_read_partition(bad)
                descriptor = json.loads(parts[0])
                descriptor["query"] = "DELETE FROM it_partition_python_empty"
                with pytest.raises(dbapi.NotSupportedError):
                    cur.adbc_read_partition(json.dumps(descriptor).encode())
                cur.execute("ALTER TABLE it_partition_python_empty ADD COLUMN label TEXT")
                with pytest.raises(dbapi.ProgrammingError):
                    cur.adbc_read_partition(parts[0])
            finally:
                cur.execute("DROP TABLE it_partition_python_empty")


def test_partition_connection_cancel(driver_path, db_kwargs):
    # The partition reader belongs to the reading connection, not the otherwise
    # unused DB-API cursor's statement. Cancel the connection after fetching begins.
    with connect(driver_path, {**db_kwargs, "adbc.redis.stream_batch_rows": "10"}) as conn:
        with conn.cursor() as cur:
            if os.environ.get("REDIS_PARTITION_SCAN_CAP"):
                cur.adbc_statement.set_options(**{"adbc.redis.partition_scan_cap": int(os.environ["REDIS_PARTITION_SCAN_CAP"])})
            cur.execute("DROP TABLE IF EXISTS it_partition_python_cancel")
            cur.adbc_ingest("it_partition_python_cancel", pa.table({"id": range(25000)}))
            try:
                cur.adbc_statement.set_options(**{"adbc.redis.partition_count": 1})
                parts, _ = cur.adbc_execute_partitions("SELECT * FROM it_partition_python_cancel")
                cur.adbc_read_partition(parts[0])
                reader = cur.fetch_record_batch()
                assert reader.read_next_batch().num_rows == 10
                conn.adbc_cancel()
                with pytest.raises(OSError, match="cancel"):
                    reader.read_all()
                reader.close()
            finally:
                cur.execute("DROP TABLE it_partition_python_cancel")


@pytest.mark.parametrize("populated", [False, True])
def test_incremental_lifecycle(driver_path, db_kwargs, populated):
    with connect(driver_path, db_kwargs) as conn:
        with conn.cursor() as cur:
            if os.environ.get("REDIS_PARTITION_SCAN_CAP"):
                cur.adbc_statement.set_options(**{"adbc.redis.partition_scan_cap": int(os.environ["REDIS_PARTITION_SCAN_CAP"])})
            table = "it_partition_incremental_python"
            cur.execute(f"DROP TABLE IF EXISTS {table}")
            cur.execute(f"CREATE TABLE {table} (id BIGINT)")
            try:
                if populated:
                    cur.execute(f"INSERT INTO {table} VALUES (1),(2),(3)")
                st = cur.adbc_statement
                assert st.get_option("adbc.statement.exec.incremental") == "false"
                with pytest.raises(dbapi.ProgrammingError):
                    st.set_options(**{"adbc.statement.exec.incremental": "yes"})
                st.set_options(**{"adbc.statement.exec.incremental": "true"})
                st.set_sql_query(f"SELECT id FROM {table}")
                parts, schema, count = st.execute_partitions()
                assert parts and count == -1
                expected_schema = pa.Schema._import_from_c(schema.address)
                for _ in range(3):
                    empty, schema, count = st.execute_partitions()
                    assert empty == [] and count == -1
                    assert pa.Schema._import_from_c(schema.address).equals(expected_schema)
                # Resetting SQL starts a new execution; queries are unaffected.
                cur.execute(f"SELECT id FROM {table}")
                expected = cur.fetch_arrow_table()
                st.set_sql_query(f"SELECT id FROM {table}")
                parts, _, _ = st.execute_partitions()
                tables = []
                for part in parts:
                    cur.adbc_read_partition(part)
                    tables.append(cur.fetch_arrow_table())
                assert pa.concat_tables(tables).sort_by("id").equals(expected.sort_by("id"))
                st.set_sql_query(f"SELECT id FROM {table} ORDER BY id")
                for _ in range(2):
                    with pytest.raises(dbapi.NotSupportedError):
                        st.execute_partitions()
                st.set_options(**{"adbc.statement.exec.incremental": "false"})
                st.set_sql_query(f"SELECT id FROM {table}")
                for _ in range(2):
                    assert st.execute_partitions()[0]
                with pytest.raises(dbapi.NotSupportedError):
                    st.get_option_float("adbc.statement.exec.progress")
            finally:
                cur.execute(f"DROP TABLE IF EXISTS {table}")


@pytest.mark.parametrize("stream", [False, True])
def test_partition_bound_expressions_cross_process(driver_path, db_kwargs, stream):
    import datetime

    query = """SELECT id + ? AS shifted,
        CASE WHEN label IS NULL THEN ? ELSE UPPER(label) END AS label,
        CAST(? AS DECIMAL(12,2)) AS amount, CAST(? AS BYTEA) AS payload,
        CAST(? AS TIMESTAMP WITH TIME ZONE) AS stamp, CAST(? AS TEXT) AS missing
        FROM it_partition_parameters_python WHERE id >= ?"""
    params = pa.record_batch([
        pa.array([1 << 54], pa.int64()), pa.array(["missing"]),
        pa.array([decimal.Decimal("1234.56")], pa.decimal128(12, 2)),
        pa.array([b"\x00\xffsecret"], pa.binary()),
        pa.array([datetime.datetime(2024, 1, 1, tzinfo=datetime.timezone.utc)], pa.timestamp("us", "UTC")),
        pa.array([None], pa.string()), pa.array([2], pa.int64()),
    ], names=[str(i) for i in range(7)])
    with connect(driver_path, db_kwargs) as conn:
        with conn.cursor() as cur:
            if os.environ.get("REDIS_PARTITION_SCAN_CAP"):
                cur.adbc_statement.set_options(**{"adbc.redis.partition_scan_cap": int(os.environ["REDIS_PARTITION_SCAN_CAP"])})
            table = "it_partition_parameters_python"
            cur.execute(f"DROP TABLE IF EXISTS {table}")
            cur.adbc_ingest(table, pa.table({"id": [1, 2, 3, 4], "label": ["a", None, "c", "d"]}))
            try:
                cur.execute(query, params)
                expected = cur.fetch_arrow_table().sort_by("shifted")
                st = cur.adbc_statement
                st.set_sql_query(query)
                st.set_options(**{"adbc.redis.partition_scan_cap": min(2, int(os.environ.get("REDIS_PARTITION_SCAN_CAP", "2")))})
                if stream:
                    st.bind_stream(pa.RecordBatchReader.from_batches(params.schema, [params]))
                else:
                    st.bind(params)
                parts, schema, _ = st.execute_partitions()
                planned_schema = pa.Schema._import_from_c(schema.address)
            except BaseException:
                cur.execute(f"DROP TABLE IF EXISTS {table}")
                raise
    try:
        tables = []
        for part in parts:
            result = subprocess.run(
                [sys.executable, "-c", READ_PARTITION],
                input=json.dumps({"driver": driver_path, "db": worker_kwargs(db_kwargs),
                                  "partition": base64.b64encode(part).decode()}),
                text=True, capture_output=True, check=True, timeout=60,
            )
            tables.append(pa.ipc.open_stream(base64.b64decode(result.stdout)).read_all())
        actual = pa.concat_tables(tables).sort_by("shifted")
        assert actual.schema.equals(planned_schema)
        assert actual.equals(expected)
        with connect(driver_path, db_kwargs) as conn:
            with conn.cursor() as cur:
                if os.environ.get("REDIS_PARTITION_SCAN_CAP"):
                    cur.adbc_statement.set_options(**{"adbc.redis.partition_scan_cap": int(os.environ["REDIS_PARTITION_SCAN_CAP"])})
                for rows in (0, 2):
                    cur.adbc_statement.set_sql_query(query)
                    batch = pa.Table.from_batches([params] * rows, schema=params.schema).combine_chunks()
                    if rows:
                        cur.adbc_statement.bind(batch.to_batches()[0])
                    else:
                        cur.adbc_statement.bind(params.slice(0, 0))
                    with pytest.raises(dbapi.ProgrammingError):
                        cur.adbc_statement.execute_partitions()
                d = json.loads(parts[0])
                d["params"][0]["data"] = base64.b64encode(b"1.5").decode()
                with pytest.raises(dbapi.ProgrammingError, match="invalid Redis ADBC partition"):
                    cur.adbc_read_partition(json.dumps(d).encode())
    finally:
        with connect(driver_path, db_kwargs) as conn:
            with conn.cursor() as cur:
                if os.environ.get("REDIS_PARTITION_SCAN_CAP"):
                    cur.adbc_statement.set_options(**{"adbc.redis.partition_scan_cap": int(os.environ["REDIS_PARTITION_SCAN_CAP"])})
                cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_balanced_partition_strategy(driver_path, db_kwargs):
    import time

    table = "it_partition_balanced_python"
    with connect(driver_path, db_kwargs) as conn:
        with conn.cursor() as cur:
            cur.execute(f"DROP TABLE IF EXISTS {table}")
            cur.adbc_ingest(table, pa.table({"id": range(5000)}))
            try:
                cur.execute(f"DELETE FROM {table} WHERE id < 4500")
                st = cur.adbc_statement
                assert st.get_option("adbc.redis.partition_strategy") == "rowid"
                with pytest.raises(dbapi.ProgrammingError):
                    st.set_options(**{"adbc.redis.partition_strategy": "auto"})
                st.set_options(**{
                    "adbc.redis.partition_strategy": "balanced",
                    "adbc.redis.partition_count": 4,
                    "adbc.redis.partition_scan_cap": int(os.environ.get("REDIS_PARTITION_SCAN_CAP", "0")),
                })
                assert st.get_option("adbc.redis.partition_strategy") == "balanced"
                # Retry only the documented transient readiness error. Each
                # DB-API call resets SQL/discovery; other failures remain fatal.
                deadline = time.monotonic() + 10
                while True:
                    try:
                        parts, schema = cur.adbc_execute_partitions(f"SELECT id FROM {table}")
                        break
                    except dbapi.ProgrammingError as exc:
                        if "still indexing" not in str(exc) or time.monotonic() >= deadline:
                            raise
                        time.sleep(0.01)
                assert len(parts) == 4
                actual = []
                for part in parts:
                    with connect(driver_path, worker_kwargs(db_kwargs)) as worker:
                        with worker.cursor() as reader:
                            reader.adbc_read_partition(part)
                            result = reader.fetch_arrow_table()
                            assert result.schema.equals(schema)
                            assert result.num_rows == 125
                            actual.extend(result.column("id").to_pylist())
                assert sorted(actual) == list(range(4500, 5000))
            finally:
                cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_pruned_partition_values(driver_path, db_kwargs):
    table = "it_partition_pruned_python"
    data = pa.table({
        "id": pa.array(range(1, 1101), pa.int64()),
        "label": [None if i % 3 == 0 else "a\x00b" for i in range(1100)],
        "amount": pa.array([decimal.Decimal("123456789.12")] * 1100, pa.decimal128(18, 2)),
        "payload": pa.array([b"\x00\xff"] * 1100, pa.binary()),
    })
    queries = [
        (f"SELECT * FROM {table} WHERE __rowid=?", pa.record_batch([pa.array([1099], pa.int64())], names=["id"]), 1),
        (f"SELECT * FROM {table} WHERE __rowid IN(1,1,2,1099)", None, 3),
        (f"SELECT * FROM {table} WHERE __rowid BETWEEN 1099 AND 1100 AND label IS NULL", None, 2),
    ]
    with connect(driver_path, db_kwargs) as owner:
        with owner.cursor() as cur:
            cur.execute(f"DROP TABLE IF EXISTS {table}")
            cur.adbc_ingest(table, data)
            cur.execute(f"DELETE FROM {table} WHERE id=2")
    try:
        for query, params, count in queries:
            with connect(driver_path, db_kwargs) as owner:
                with owner.cursor() as cur:
                    cur.execute(query, params)
                    expected = cur.fetch_arrow_table().sort_by("id")
                    st = cur.adbc_statement
                    st.set_sql_query(query)
                    st.set_options(**{"adbc.redis.partition_count": 8})
                    if params is not None:
                        st.bind(params)
                    parts, exported, _ = st.execute_partitions()
                    schema = pa.Schema._import_from_c(exported.address)
                    assert len(parts) == count
            # Planner connection is closed. All values/types come from HASHes.
            tables = []
            for part in parts:
                with connect(driver_path, worker_kwargs(db_kwargs)) as worker:
                    with worker.cursor() as reader:
                        reader.adbc_read_partition(part)
                        result = reader.fetch_arrow_table()
                        assert result.schema.equals(schema)
                        tables.append(result)
            assert pa.concat_tables(tables).sort_by("id").equals(expected)
        with connect(driver_path, {**worker_kwargs(db_kwargs), "adbc.redis.stream_batch_rows": "10"}) as worker:
            with worker.cursor() as reader:
                reader.adbc_statement.set_options(**{"adbc.redis.partition_count": 1})
                direct, _ = reader.adbc_execute_partitions(
                    f"SELECT * FROM {table} WHERE __rowid BETWEEN 1 AND 1024")
                reader.adbc_read_partition(direct[0])
                batches = reader.fetch_record_batch()
                assert batches.read_next_batch().num_rows == 10
                worker.adbc_cancel()
                with pytest.raises(OSError, match="cancel"):
                    batches.read_all()
                batches.close()
    finally:
        with connect(driver_path, db_kwargs) as owner:
            with owner.cursor() as cur:
                cur.execute(f"DROP TABLE IF EXISTS {table}")
