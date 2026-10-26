"""Immutable retained partitions through the native ADBC/Arrow boundary."""

import base64
import contextlib
import decimal
import json
import os
import queue
import re
import subprocess
import sys
import threading
import uuid

import pyarrow as pa
import pytest
import redis

from .test_partitions import READ_PARTITION, connect, worker_kwargs


@contextlib.contextmanager
def retained_namespace(db_kwargs):
    namespace = "native_" + uuid.uuid4().hex
    prefix = f"adbc:{{retained:{namespace}}}:"
    options = {**db_kwargs, "adbc.redis.partition_result_namespace": namespace}
    raw_options = {"protocol": 3}
    if ca := db_kwargs.get("adbc.redis.tls_ca_file"):
        raw_options["ssl_ca_certs"] = ca
    uri = os.environ.get("REDIS_ADMIN_URI") or db_kwargs["uri"]
    single = redis.Redis.from_url(uri, **raw_options)
    cluster = None
    try:
        try:
            clustered = bool(single.info("cluster").get("cluster_enabled"))
        except redis.RedisError:
            clustered = db_kwargs.get("adbc.redis.cluster") == "true"
        if clustered:
            cluster = redis.RedisCluster.from_url(uri, **raw_options)
        yield options
    finally:
        # Only this fresh namespace is owned by this test. Never flush or delete
        # application/source keys. All namespaces use one hash slot; scanning all
        # primaries also covers a changed topology during cleanup.
        clients = ([cluster.get_redis_connection(node) for node in cluster.get_primaries()]
                   if cluster is not None else [single])
        for client in clients:
            keys = list(client.scan_iter(match=prefix + "*", count=100))
            for key in keys:
                (cluster or single).delete(key)
            assert not list(client.scan_iter(match=prefix + "*", count=100))
        if cluster is not None:
            cluster.close()
        single.close()


def read_in_process(driver_path, options, partition):
    result = subprocess.run([sys.executable, "-c", READ_PARTITION],
        input=json.dumps({"driver": driver_path, "db": options,
                          "partition": base64.b64encode(partition).decode()}),
        text=True, capture_output=True, timeout=60)
    assert result.returncode == 0, "independent retained worker failed"
    return pa.ipc.open_stream(base64.b64decode(result.stdout)).read_all()


def test_retained_partitions_native_replay_types_and_revoke(driver_path, db_kwargs):
    table = "it_retained_native_types"
    data = pa.table({
        "id": pa.array([-1, 7, 9, 2**53 + 1, 2**63 - 1], pa.int64()),
        "label": pa.array(["exact\x00", None, "é", "a", "b"]),
        "payload": pa.array([b"\x00\xff", None, b"", b"binary", b"\xff"], pa.binary()),
        "amount": pa.array([decimal.Decimal("1.23"), None, decimal.Decimal("-2.00"),
                            decimal.Decimal("3.45"), decimal.Decimal("9.99")], pa.decimal128(20, 2)),
    })
    with retained_namespace(db_kwargs) as options:
        parts = []
        try:
            with connect(driver_path, options) as planner:
                with planner.cursor() as cur:
                    cur.execute(f"DROP TABLE IF EXISTS {table}")
                    cur.adbc_ingest(table, data)
                    cur.execute(f"SELECT * FROM {table} ORDER BY id")
                    expected = cur.fetch_arrow_table()
                    cur.adbc_statement.set_options(**{
                        "adbc.redis.partition_result_mode": "retained",
                        "adbc.redis.partition_count": 2,
                        "adbc.redis.stream_batch_rows": 2,
                    })
                    parts, schema = cur.adbc_execute_partitions(f"SELECT * FROM {table} ORDER BY id")
                    assert schema.equals(expected.schema)
                    assert len(parts) == 2
            # No planning handles remain; DML and DDL cannot change replay.
            with connect(driver_path, options) as source:
                with source.cursor() as cur:
                    cur.execute(f"UPDATE {table} SET label='changed' WHERE id=7")
                    cur.execute(f"DELETE FROM {table} WHERE id=-1")
                    cur.execute(f"INSERT INTO {table} (id) VALUES (100)")
                    cur.execute(f"DROP TABLE {table}")
            worker = {**worker_kwargs(options), "adbc.redis.partition_reader_only": "true"}
            for _ in range(2):
                actual = pa.concat_tables([read_in_process(driver_path, worker, part) for part in parts])
                assert actual.schema.equals(expected.schema)
                assert actual.equals(expected)
            with connect(driver_path, options) as revoker:
                with revoker.cursor() as cur:
                    job = json.loads(parts[0])["result"]["job_id"]
                    assert re.fullmatch("[0-9a-f]{32}", job)
                    cur.execute(f"DROP PARTITION RESULT '{job}'")
            with connect(driver_path, worker) as reader:
                with reader.cursor() as cur:
                    with pytest.raises(dbapi_error_types(), match="revoked"):
                        cur.adbc_read_partition(parts[0])
                    with pytest.raises(dbapi_error_types(), match="reader-only"):
                        cur.execute("SELECT 1")
        finally:
            with connect(driver_path, options) as cleanup:
                with cleanup.cursor() as cur:
                    cur.execute(f"DROP TABLE IF EXISTS {table}")


def dbapi_error_types():
    import adbc_driver_manager.dbapi as dbapi
    return (dbapi.Error,)


def test_retained_partitions_native_byte_budget(driver_path, db_kwargs):
    data = pa.table({"id": pa.array([1, 2, 3, 4, 5], pa.int64()),
                     "label": ["a\x00é", None, "x" * 8192, "last", "tail"],
                     "payload": pa.array([b"\x00\xff"] * 5, pa.binary())})
    table = "it_retained_native_bytes"
    with retained_namespace(db_kwargs) as options:
        try:
            with connect(driver_path, options) as planner, planner.cursor() as cur:
                cur.execute(f"DROP TABLE IF EXISTS {table}")
                cur.adbc_ingest(table, data)
                for planner_budget in (0, 1):
                    cur.adbc_statement.set_options(**{
                        "adbc.redis.partition_result_mode": "retained",
                        "adbc.redis.partition_count": 10,
                        "adbc.redis.stream_batch_rows": 100,
                        "adbc.redis.stream_batch_bytes": planner_budget,
                    })
                    parts, _ = cur.adbc_execute_partitions(f"SELECT * FROM {table} ORDER BY id")
                    assert len(parts) == (5 if planner_budget else 1)
                    for worker_budget in (0, 1, 1600):
                        worker = {**worker_kwargs(options),
                                  "adbc.redis.partition_reader_only": "true",
                                  "adbc.redis.stream_batch_rows": "3",
                                  "adbc.redis.stream_batch_bytes": str(worker_budget)}
                        batches = []
                        with connect(driver_path, worker) as reader, reader.cursor() as read:
                            for part in parts:
                                read.adbc_read_partition(part)
                                with read.fetch_record_batch() as stream:
                                    for batch in stream:
                                        assert 0 < batch.num_rows <= 3
                                        cost = sum(768 + 2 * len((row['label'] or '').encode()) +
                                                   2 * len(row['payload']) for row in batch.to_pylist())
                                        assert not worker_budget or batch.num_rows == 1 or cost <= worker_budget
                                        batches.append(batch)
                        assert pa.Table.from_batches(batches).equals(data)
                    job = json.loads(parts[0])["result"]["job_id"]
                    cur.execute(f"DROP PARTITION RESULT '{job}'")
        finally:
            with connect(driver_path, options) as cleanup, cleanup.cursor() as cur:
                cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_retained_partitions_native_planner_exit_and_worker_death(driver_path, db_kwargs):
    table = "it_retained_native_process"
    data = pa.table({"id": pa.array([-1, 2**53 + 1, 2**63 - 1], pa.int64()),
                     "label": ["exact\x00", None, "frozen"]})
    planner_code = r'''
import base64, json, os, sys
import adbc_driver_manager.dbapi as dbapi
p=json.load(sys.stdin)
with dbapi.connect(driver=p['driver'],db_kwargs=p['db'],autocommit=True) as conn:
    with conn.cursor() as cur:
        cur.adbc_statement.set_options(**{'adbc.redis.partition_result_mode':'retained',
            'adbc.redis.partition_count':1,'adbc.redis.stream_batch_rows':1})
        parts,_=cur.adbc_execute_partitions('SELECT * FROM it_retained_native_process ORDER BY id')
        assert len(parts)==1
        print(base64.b64encode(parts[0]).decode(),flush=True)
        os._exit(77)
'''
    partial_code = r'''
import base64,json,sys,threading
import adbc_driver_manager.dbapi as dbapi
p=json.load(sys.stdin)
with dbapi.connect(driver=p['driver'],db_kwargs={**p['db'],'adbc.redis.stream_batch_rows':'1'},autocommit=True) as conn:
    with conn.cursor() as cur:
        cur.adbc_read_partition(base64.b64decode(p['partition']))
        with cur.fetch_record_batch() as reader:
            assert reader.read_next_batch().num_rows==1
            print('first-batch',flush=True)
            threading.Event().wait()
'''
    with retained_namespace(db_kwargs) as options:
        try:
            with connect(driver_path, options) as source:
                with source.cursor() as cur:
                    cur.execute(f"DROP TABLE IF EXISTS {table}")
                    cur.adbc_ingest(table, data)
                    cur.execute(f"SELECT * FROM {table} ORDER BY id")
                    expected = cur.fetch_arrow_table()
            plan = subprocess.run([sys.executable, "-c", planner_code],
                input=json.dumps({"driver": driver_path, "db": options}),
                text=True, capture_output=True, timeout=60)
            assert plan.returncode == 77, "planner did not exit after publication"
            part = base64.b64decode(plan.stdout.strip())
            worker = {**worker_kwargs(options), "adbc.redis.partition_reader_only": "true"}
            payload = json.dumps({"driver": driver_path, "db": worker,
                                  "partition": base64.b64encode(part).decode()})
            process = subprocess.Popen([sys.executable, "-c", partial_code], stdin=subprocess.PIPE,
                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
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
            with connect(driver_path, options) as source:
                with source.cursor() as cur:
                    cur.execute(f"DROP TABLE {table}")
            for _ in range(2):
                assert read_in_process(driver_path, worker, part).equals(expected)
        finally:
            with connect(driver_path, options) as cleanup:
                with cleanup.cursor() as cur:
                    cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_retained_partitions_native_global_sql_equivalence(driver_path, db_kwargs):
    """Global operators run once, then ordinal chunks preserve exact Arrow output."""
    table = "it_retained_native_global"
    view = "it_retained_native_global_view"
    data = pa.table({
        "id": pa.array([-1, 7, 9, 2**53 + 1, 2**63 - 1], pa.int64()),
        "grp": pa.array([1, 1, 2, 2, 3], pa.int32()),
        "label": [None, None, "duplicate", "duplicate", "last"],
        "amount": pa.array([None, None, decimal.Decimal("1.25"),
                            decimal.Decimal("2.50"), decimal.Decimal("3.75")],
                           pa.decimal128(20, 2)),
    })
    queries = [
        f"SELECT * FROM {table} ORDER BY grp,id DESC LIMIT 3 OFFSET 1",
        f"SELECT DISTINCT label FROM {table} ORDER BY label NULLS FIRST",
        f"SELECT DISTINCT ON (grp) grp,id FROM {table} ORDER BY grp,id DESC",
        f"SELECT grp,COUNT(*),COUNT(amount),SUM(amount),AVG(amount),MIN(id),MAX(id) "
        f"FROM {table} GROUP BY grp HAVING COUNT(*)>=1 ORDER BY grp",
        f"SELECT id,ROW_NUMBER() OVER (PARTITION BY grp ORDER BY id) AS pos,"
        f"SUM(amount) OVER (PARTITION BY grp) AS total FROM {table} QUALIFY pos<=1 ORDER BY id",
        f"SELECT grp FROM {table} UNION SELECT grp FROM {table} ORDER BY grp",
        f"SELECT grp FROM {table} UNION ALL SELECT grp FROM {table} ORDER BY grp",
        f"SELECT grp FROM {table} INTERSECT SELECT grp FROM {table} WHERE grp=2",
        f"SELECT grp FROM {table} EXCEPT SELECT grp FROM {table} WHERE grp<>2",
        f"SELECT a.id,b.id AS other FROM {table} a JOIN {table} b ON a.grp=b.grp ORDER BY a.id,b.id",
        f"WITH a AS (SELECT grp,COUNT(*) AS n FROM {table} GROUP BY grp) SELECT * FROM a ORDER BY grp",
        f"SELECT id FROM {table} WHERE grp IN (SELECT grp FROM {table} GROUP BY grp HAVING COUNT(*)=2) ORDER BY id",
        f"SELECT a.id,(SELECT COUNT(*) FROM {table} b WHERE b.grp=a.grp) AS n FROM {table} a ORDER BY a.id",
        f"SELECT * FROM {view} ORDER BY grp",
        f"SELECT ARRAY_AGG(id ORDER BY id),SUM(amount),COUNT(*) FROM {table}",
        f"SELECT * FROM {table} WHERE false",
        f"SELECT * FROM {table} LIMIT 0",
    ]
    with retained_namespace(db_kwargs) as options:
        try:
            with connect(driver_path, options) as planner:
                with planner.cursor() as cur:
                    cur.execute(f"DROP VIEW IF EXISTS {view}")
                    cur.execute(f"DROP TABLE IF EXISTS {table}")
                    cur.adbc_ingest(table, data)
                    cur.execute(f"CREATE VIEW {view} AS SELECT grp,SUM(amount) AS amount FROM {table} GROUP BY grp")
                    cur.adbc_statement.set_options(**{
                        "adbc.redis.partition_result_mode": "retained",
                        "adbc.redis.partition_count": 3,
                        "adbc.redis.stream_batch_rows": 2,
                    })
                    worker = {**worker_kwargs(options), "adbc.redis.partition_reader_only": "true"}
                    with connect(driver_path, worker) as consumer:
                        with consumer.cursor() as fetch:
                            for query in queries:
                                cur.execute(query)
                                expected = cur.fetch_arrow_table()
                                parts, schema = cur.adbc_execute_partitions(query)
                                actual_parts = []
                                for part in parts:
                                    fetch.adbc_read_partition(part)
                                    actual_parts.append(fetch.fetch_arrow_table())
                                actual = pa.concat_tables(actual_parts)
                                assert schema.equals(expected.schema), query
                                assert actual.schema.equals(expected.schema), query
                                assert actual.equals(expected), query
        finally:
            with connect(driver_path, options) as cleanup:
                with cleanup.cursor() as cur:
                    cur.execute(f"DROP VIEW IF EXISTS {view}")
                    cur.execute(f"DROP TABLE IF EXISTS {table}")
