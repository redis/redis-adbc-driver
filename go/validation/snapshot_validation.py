# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0 (the "License");
# you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
"""Native C/ADBC snapshot validation on REDIS_SNAPSHOT_URI (DISPOSABLE).

This script clears the explicitly selected test database before and after each
test. Run separately from the ordinary validation suite; see docs/snapshots.md.
"""

import os
import threading
from concurrent.futures import ThreadPoolExecutor
from decimal import Decimal
import sys
import time
import unittest
from pathlib import Path

import adbc_driver_manager.dbapi
import pyarrow as pa
import redis


@unittest.skipUnless(os.environ.get("REDIS_SNAPSHOT_URI"), "set REDIS_SNAPSHOT_URI to a disposable database")
class SnapshotValidation(unittest.TestCase):
    def setUp(self):
        self.uri = os.environ["REDIS_SNAPSHOT_URI"]
        kwargs = {"decode_responses": True}
        if self.uri.startswith("rediss://") and os.environ.get("SSL_CERT_FILE"):
            kwargs["ssl_ca_certs"] = os.environ["SSL_CERT_FILE"]
        self.raw = redis.Redis.from_url(self.uri, **kwargs)
        self.addCleanup(self.raw.close)
        self.reset()
        self.addCleanup(self.reset)
        ext = {"darwin": "dylib", "win32": "dll"}.get(sys.platform, "so")
        self.driver = str(Path(__file__).resolve().parents[1] / f"build/libadbc_driver_redis.{ext}")
        self.conn = self.connect()
        self.addCleanup(self.conn.close)

    def reset(self):
        for index in self.raw.execute_command("FT._LIST"):
            self.raw.execute_command("FT.DROPINDEX", index)
        self.raw.flushdb()

    def connect(self, **options):
        return adbc_driver_manager.dbapi.connect(
            driver=self.driver,
            db_kwargs={"uri": self.uri, **options},
            autocommit=True,
        )

    def execute_retry(self, cur, sql):
        # Explicit client retries for deterministic test statements. The driver
        # never replays SQL; background publication/backpressure can return 40001.
        deadline = time.monotonic() + 20
        while True:
            try:
                cur.execute(sql)
                return
            except adbc_driver_manager.Error as exc:
                state = getattr(exc, "sqlstate", None)
                if isinstance(state, bytes):
                    state = state.decode()
                if state != "40001" or time.monotonic() >= deadline:
                    raise
                time.sleep(0.005)

    def test_segmented_ordered_late_projection(self):
        with self.conn.cursor() as cur:
            cur.execute("CREATE TABLE t(id INTEGER,url VARCHAR,payload VARCHAR,spare VARCHAR)")
            cur.execute("INSERT INTO t SELECT i,CASE WHEN i%2=0 THEN 'match' ELSE 'miss' END,'row-'||i||repeat('p',4096),NULL FROM generate_series(1,600) AS g(i)")
            cur.execute("UPDATE t SET url=CASE WHEN id=2 THEN 'miss' ELSE url END,payload=CASE WHEN id=4 THEN 'changed' ELSE payload END,spare=CASE WHEN id=4 THEN 'x' ELSE spare END WHERE id IN (2,4)")
            cur.execute("DELETE FROM t WHERE id=6")
            for stream in ("true", "false"):
                cur.adbc_statement.set_options(**{
                    "adbc.redis.stream_results": stream,
                    "adbc.redis.stream_batch_rows": "1",
                })
                for tail, ids in (
                    ("ORDER BY id LIMIT 2 OFFSET 1", [8, 10]),
                    ("ORDER BY id DESC NULLS LAST LIMIT 2 OFFSET 1", [598, 596]),
                    ("ORDER BY id LIMIT 1", [4]),
                    ("ORDER BY id LIMIT 0", []),
                    ("ORDER BY id LIMIT 2 OFFSET 1000", []),
                ):
                    cur.execute("SELECT id,payload,spare FROM t WHERE url LIKE '%match%' " + tail)
                    expected = [(i, "changed", "x") if i == 4 else
                                (i, f"row-{i}" + "p" * 4096, None) for i in ids]
                    self.assertEqual(cur.fetchall(), expected)

    def test_stream_survives_update_and_drop(self):
        count, exact = 12001, 9007199254740993
        with self.conn.cursor() as cur:
            self.execute_retry(cur, "CREATE TABLE t(id INTEGER,v BIGINT,a VARCHAR,b VARCHAR,c VARCHAR)")
            table = pa.table({
                "id": pa.array(range(count), pa.int32()),
                "v": pa.array([exact] * count, pa.int64()),
                "a": ["a"] * count, "b": ["b"] * count, "c": ["c"] * count,
            })
            self.assertEqual(cur.adbc_ingest("t", table, mode="append"), count)
        other = self.connect()
        self.addCleanup(other.close)
        with self.conn.cursor() as reader, other.cursor() as writer:
            reader.adbc_statement.set_options(**{"adbc.redis.stream_batch_rows": "7"})
            reader.execute("SELECT id,v FROM t")
            self.assertEqual(reader.rowcount, -1)
            first = reader.fetchmany(7)
            self.execute_retry(writer, "UPDATE t SET v=42")
            writer.execute("SELECT count(*),min(v),max(v) FROM t")
            self.assertEqual(writer.fetchall(), [(count, 42, 42)])
            self.execute_retry(writer, "DROP TABLE t")
            rows = first + reader.fetchall()
            self.assertEqual(rows, [(i, exact) for i in range(count)])
        self.assertFalse(any(key.startswith("p:") for key in self.raw.hkeys("adbc:{meta}:snapshot:catalog")))

    def test_repeated_append_admission_without_caller_retry(self):
        count, exact = 2001, 9007199254740993
        with self.conn.cursor() as cur:
            cur.execute("CREATE TABLE t(id INTEGER,v BIGINT)")
            base = pa.table(
                {
                    "id": pa.array(range(count), pa.int32()),
                    "v": pa.array([exact] * count, pa.int64()),
                }
            )
            self.assertEqual(cur.adbc_ingest("t", base, mode="append"), count)
            for i in range(20):
                batch = pa.table(
                    {
                        "id": pa.array([count + i], pa.int32()),
                        "v": pa.array([exact + i], pa.int64()),
                    }
                )
                # Each independent Arrow input is consumed once; ordinary
                # admission must make room before touching it, without retries.
                self.assertEqual(cur.adbc_ingest("t", batch, mode="append"), 1)
            cur.execute("SELECT id,v FROM t ORDER BY id")
            expected = [(i, exact) for i in range(count)]
            expected += [(count + i, exact + i) for i in range(20)]
            self.assertEqual(cur.fetchall(), expected)

    def test_concurrent_independent_ingest_consumes_input_once(self):
        names = ("append_a", "append_b")
        with self.conn.cursor() as cur:
            for name in names:
                cur.execute(f"CREATE TABLE {name}(id INTEGER,v BIGINT)")
        ready = threading.Barrier(2, timeout=20)
        consumed = {name: [] for name in names}
        schema = pa.schema([("id", pa.int32()), ("v", pa.int64())])
        exact = 9007199254740993

        def append(name):
            def batches():
                for index in range(2):
                    if index == 1:
                        # Both native workspaces have consumed their first batch
                        # before either stream can complete and publish.
                        ready.wait()
                    consumed[name].append(index)
                    yield pa.RecordBatch.from_arrays(
                        [pa.array([index + 1], pa.int32()),
                         pa.array([exact + index], pa.int64())],
                        schema=schema,
                    )

            with self.connect() as conn, conn.cursor() as cur:
                return cur.adbc_ingest(
                    name, pa.RecordBatchReader.from_batches(schema, batches()), mode="append"
                )

        with ThreadPoolExecutor(max_workers=2) as pool:
            futures = [pool.submit(append, name) for name in names]
            self.assertEqual([future.result(timeout=40) for future in futures], [2, 2])
        self.assertEqual(consumed, {name: [0, 1] for name in names})
        with self.conn.cursor() as cur:
            for name in names:
                cur.execute(f"SELECT id,v FROM {name} ORDER BY id")
                self.assertEqual(cur.fetchall(), [(1, exact), (2, exact + 1)])

    def test_bound_decimal_scales_share_one_exact_output_schema(self):
        values = [Decimal("0.0001"), Decimal("100000000"), None]
        expected = [Decimal("10000"), Decimal("0.00000001"), None]
        with self.conn.cursor() as cur:
            for reverse in (False, True):
                params = pa.table({"": pa.array(
                    values[::-1] if reverse else values, type=pa.decimal128(20, 4)
                )})
                cur.execute("SELECT CAST(1 AS NUMERIC(10,0)) / $1 AS quotient", params)
                result = cur.fetch_arrow_table()
                self.assertEqual(result.schema.field(0).type, pa.decimal128(33, 28))
                self.assertEqual(result.column(0).to_pylist(), expected[::-1] if reverse else expected)

    def test_concurrent_same_target_ingest_consumes_input_once(self):
        schema = pa.schema([("id", pa.int32()), ("v", pa.int64()), ("s", pa.string())])
        exact = 9007199254740993
        for mode in ("append", "create_append"):
            with self.subTest(mode=mode):
                name = "same_" + mode
                if mode == "append":
                    with self.conn.cursor() as cur:
                        cur.execute(f"CREATE TABLE {name}(id INTEGER,v BIGINT,s TEXT)")
                ready = threading.Barrier(4, timeout=20)
                consumed = [[] for _ in range(4)]

                def append(worker):
                    def batches():
                        for index in range(2):
                            consumed[worker].append(index)
                            row = worker * 2 + index
                            yield pa.RecordBatch.from_arrays(
                                [pa.array([row], pa.int32()),
                                 pa.array([exact + row], pa.int64()),
                                 pa.array([None if index == 0 else str(worker)], pa.string())],
                                schema=schema,
                            )

                    with self.connect() as conn, conn.cursor() as cur:
                        # Synchronize before admission: waiting inputs must stay
                        # unread, so a barrier inside the generator would deadlock.
                        ready.wait()
                        return cur.adbc_ingest(
                            name, pa.RecordBatchReader.from_batches(schema, batches()), mode=mode
                        )

                with ThreadPoolExecutor(max_workers=4) as pool:
                    futures = [pool.submit(append, worker) for worker in range(4)]
                    self.assertEqual([future.result(timeout=40) for future in futures], [2] * 4)
                self.assertEqual(consumed, [[0, 1] for _ in range(4)])
                with self.conn.cursor() as cur:
                    cur.execute(f"SELECT id,v,s FROM {name} ORDER BY id")
                    self.assertEqual(cur.fetchall(), [
                        (row, exact + row, None if row % 2 == 0 else str(row // 2))
                        for row in range(8)
                    ])
                    cur.execute(f"SELECT COUNT(*),COUNT(DISTINCT __rowid),MIN(__rowid),MAX(__rowid) FROM {name}")
                    self.assertEqual(cur.fetchall(), [(8, 8, 1, 8)])

    def test_late_ingest_error_does_not_publish(self):
        with self.conn.cursor() as cur:
            self.execute_retry(cur, "CREATE TABLE t(v INTEGER NOT NULL)")
            schema = pa.schema([pa.field("v", pa.int32())])
            batches = [pa.record_batch([pa.array(values, pa.int32())], schema=schema)
                       for values in ([7], [None])]
            before = self.raw.hget("adbc:{meta}:snapshot:catalog", "root")
            with self.assertRaises(adbc_driver_manager.IntegrityError):
                cur.adbc_ingest("t", pa.RecordBatchReader.from_batches(schema, batches), mode="append")
            self.assertEqual(self.raw.hget("adbc:{meta}:snapshot:catalog", "root"), before)
            cur.execute("SELECT count(*) FROM t")
            self.assertEqual(cur.fetchall(), [(0,)])
            temporary_schema = pa.schema([pa.field("v", pa.int32(), nullable=False)])
            temporary_batches = [pa.record_batch([pa.array(values, pa.int32())], schema=temporary_schema)
                                 for values in ([7], [None])]
            with self.assertRaises(adbc_driver_manager.IntegrityError):
                cur.adbc_ingest("t", pa.RecordBatchReader.from_batches(temporary_schema, temporary_batches),
                                mode="create", temporary=True)
            self.assertEqual(self.raw.hget("adbc:{meta}:snapshot:catalog", "root"), before)
            cur.execute("SELECT count(*) FROM t")
            self.assertEqual(cur.fetchall(), [(0,)])

    def test_bound_query_and_schema(self):
        with self.conn.cursor() as cur:
            self.execute_retry(cur, "CREATE TABLE t(v BIGINT)")
            self.execute_retry(cur, "INSERT INTO t VALUES (9007199254740993)")
            cur.execute("SELECT v FROM t WHERE v=?", (9007199254740993,))
            self.assertEqual(cur.fetchall(), [(9007199254740993,)])
            self.assertEqual(self.conn.adbc_get_table_schema("t").field("v").type, pa.int64())

    def test_delta_visibility_and_compaction(self):
        with self.conn.cursor() as cur:
            self.execute_retry(cur, "CREATE TABLE t(id INTEGER,v BIGINT,payload VARCHAR)")
            self.execute_retry(cur, "INSERT INTO t VALUES(1,10,'first'),(2,20,'second'),(3,30,'third')")
            self.execute_retry(cur, "UPDATE t SET v=9007199254740993,payload=NULL WHERE __rowid=1")
            self.execute_retry(cur, "DELETE FROM t WHERE __rowid=2")
            cur.execute("SELECT id FROM t WHERE v<100 ORDER BY id LIMIT 1")
            self.assertEqual(cur.fetchall(), [(3,)])
            cur.execute("SELECT count(*),sum(v) FROM t")
            self.assertEqual(cur.fetchall(), [(2, 9007199254741023)])
            for value in range(100, 110):
                self.execute_retry(cur, f"UPDATE t SET v={value} WHERE __rowid=1")
            cur.execute("SELECT id,v,payload FROM t ORDER BY id")
            self.assertEqual(cur.fetchall(), [(1, 109, None), (3, 30, "third")])
            self.execute_retry(cur, "INSERT INTO t VALUES(4,40,'fourth')")
            cur.execute("SELECT __rowid,id FROM t ORDER BY id")
            self.assertEqual(cur.fetchall(), [(1, 1), (3, 3), (4, 4)])

    def test_delta_stream_survives_compaction(self):
        with self.conn.cursor() as cur:
            self.execute_retry(cur, "CREATE TABLE t(id INTEGER,v INTEGER)")
            self.execute_retry(cur, "INSERT INTO t SELECT i,10 FROM generate_series(1,1001) AS g(i)")
            self.execute_retry(cur, "UPDATE t SET v=11 WHERE __rowid=1")
        other = self.connect()
        self.addCleanup(other.close)
        with self.conn.cursor() as reader, other.cursor() as writer:
            reader.adbc_statement.set_options(**{"adbc.redis.stream_batch_rows": "7"})
            reader.execute("SELECT id,v FROM t")
            self.assertEqual(reader.rowcount, -1)
            first = reader.fetchmany(7)
            self.execute_retry(writer, "DELETE FROM t WHERE __rowid=2")
            for value in range(100, 110):
                self.execute_retry(writer, f"UPDATE t SET v={value} WHERE __rowid=1")
            self.assertEqual(first + reader.fetchall(),
                             [(i, 11 if i == 1 else 10) for i in range(1, 1002)])
            writer.execute("SELECT count(*),min(v),max(v) FROM t")
            self.assertEqual(writer.fetchall(), [(1000, 10, 109)])

    def test_alter_and_temporary_objects(self):
        other = self.connect()
        self.addCleanup(other.close)
        with self.conn.cursor() as cur, other.cursor() as reader:
            self.execute_retry(cur, "CREATE TABLE t(id INTEGER,v INTEGER)")
            self.execute_retry(cur, "INSERT INTO t VALUES (1,10),(2,20)")
            self.execute_retry(cur, "ALTER TABLE t ADD COLUMN n INTEGER DEFAULT 7 NOT NULL")
            self.execute_retry(cur, "ALTER TABLE t ALTER COLUMN v TYPE VARCHAR USING 'v=' || v")
            cur.execute("SELECT * FROM t ORDER BY id")
            self.assertEqual(cur.fetchall(), [(1, "v=10", 7), (2, "v=20", 7)])
            self.execute_retry(cur, "CREATE TEMP TABLE t(v INTEGER)")
            self.execute_retry(cur, "INSERT INTO t VALUES (42)")
            cur.execute("SELECT * FROM t")
            self.assertEqual(cur.fetchall(), [(42,)])
            reader.execute("SELECT * FROM t ORDER BY id")
            self.assertEqual(reader.fetchall(), [(1, "v=10", 7), (2, "v=20", 7)])

    def test_foreign_copy_explicit_refresh(self):
        self.raw.hset("app:1", mapping={"v": "10"})
        self.raw.execute_command("FT.CREATE", "app_index", "ON", "HASH", "PREFIX", 1,
                                 "app:", "SCHEMA", "v", "NUMERIC")
        foreign = self.connect(**{"adbc.redis.foreign_indexes": "true"})
        self.addCleanup(foreign.close)
        with foreign.cursor() as cur:
            cur.execute("SELECT __key,v FROM redis_foreign.app_index WHERE __key=?", ("app:1",))
            table = cur.fetch_arrow_table()
            self.assertEqual(table["__key"].type, pa.string())
            self.assertEqual(table.to_pylist(), [{"__key": "app:1", "v": 10}])
            cur.execute("SELECT v FROM redis_foreign.app_index")
            self.assertEqual(cur.fetchall(), [(10,)])
            self.raw.hset("app:1", "v", "99")
            cur.execute("SELECT v FROM redis_foreign.app_index")
            self.assertEqual(cur.fetchall(), [(10,)])
            foreign.adbc_connection.set_options(**{"adbc.redis.foreign_indexes_refresh": "true"})
            cur.execute("SELECT v FROM redis_foreign.app_index")
            self.assertEqual(cur.fetchall(), [(99,)])
            cur.execute("SELECT __key,v FROM redis_foreign.app_index")
            self.assertEqual(cur.fetchall(), [("app:1", 99)])
            cur.execute("SELECT * FROM redis_foreign.app_index")
            self.assertEqual([item[0] for item in cur.description], ["v"])
            self.assertEqual(cur.fetchall(), [(99,)])
            self.assertEqual(self.raw.hget("app:1", "v"), "99")
            self.raw.execute_command("FT.INFO", "app_index")

    def test_foreign_source_key_residual_limit(self):
        self.raw.hset("app:origin", mapping={"v": "1", "a": "a", "b": "b", "c": "c"})
        self.raw.execute_command("FT.CREATE", "app_index", "ON", "HASH", "PREFIX", 1,
                                 "app:", "SCHEMA", "v", "NUMERIC", "a", "TAG", "b", "TAG", "c", "TAG")
        foreign = self.connect(**{"adbc.redis.foreign_indexes": "true"})
        self.addCleanup(foreign.close)
        with foreign.cursor() as cur:
            cur.execute("SELECT __key FROM redis_foreign.app_index WHERE LENGTH(a)=1 ORDER BY a LIMIT 1")
            table = cur.fetch_arrow_table()
            self.assertEqual(table.schema, pa.schema([("__key", pa.string())]))
            self.assertEqual(table.to_pylist(), [{"__key": "app:origin"}])

    def test_wide_multi_page_stream_retains_all_exact_columns(self):
        count, exact = 4097, 9007199254740993
        values = {f"c{j}": [i*100+j for i in range(count)] for j in range(64)}
        values["c0"] = list(range(count))
        values["c1"] = [exact+i for i in range(count)]
        values["c2"] = [None if i%7 == 0 else i*100+2 for i in range(count)]
        source = pa.table({name: pa.array(column, pa.int64()) for name, column in values.items()})
        with self.conn.cursor() as cur:
            self.assertEqual(cur.adbc_ingest("native_wide", source, mode="create"), count)
            cur.execute("UPDATE native_wide SET c1=-9007199254740993 WHERE c0=10")
            cur.execute("DELETE FROM native_wide WHERE c0=11")
        values["c1"][10] = -exact
        expected = pa.table({name: pa.array([value for i, value in enumerate(column) if i != 11], pa.int64())
                             for name, column in values.items()})
        with self.connect() as writer, self.conn.cursor() as reader, writer.cursor() as cur:
            reader.adbc_statement.set_options(**{"adbc.redis.stream_batch_rows": "127"})
            reader.execute("SELECT * FROM native_wide")
            stream = reader.fetch_record_batch()
            try:
                first = stream.read_next_batch()
                self.assertEqual(first.num_rows, 127)
                cur.execute("UPDATE native_wide SET c1=0 WHERE c0=1")
                cur.execute("DELETE FROM native_wide WHERE c0=2")
                cur.execute("TRUNCATE native_wide")
                cur.execute("DROP TABLE native_wide")
                result = pa.Table.from_batches([first, *stream], schema=stream.schema)
                self.assertTrue(result.sort_by([("c0", "ascending")]).equals(expected))
            finally:
                stream.close()

    def test_balanced_partitions_retain_visible_rows_after_producer_close(self):
        with self.connect() as producer, producer.cursor() as cur:
            cur.execute("CREATE TABLE balanced(id INTEGER,exact BIGINT,payload VARCHAR)")
            cur.execute("INSERT INTO balanced SELECT i,9007199254740993+i,"
                        "CASE WHEN i%7=0 THEN NULL ELSE 'row'||i END "
                        "FROM generate_series(0,5999) AS g(i)")
            cur.execute("DELETE FROM balanced WHERE id<5000")
            cur.adbc_statement.set_options(**{
                "adbc.redis.partition_strategy": "balanced",
                "adbc.redis.partition_count": "4",
            })
            parts, schema = cur.adbc_execute_partitions("SELECT id,exact,payload FROM balanced")
            group = cur.adbc_statement.get_option("adbc.redis.partition_snapshot_id")
            self.assertEqual(len(parts), 4)
        with self.conn.cursor() as cur:
            cur.execute("UPDATE balanced SET exact=0 WHERE id=5000")
            cur.execute("DELETE FROM balanced WHERE id=5001")
            cur.execute("INSERT INTO balanced VALUES(6000,42,'new')")
            cur.execute("TRUNCATE balanced")
            cur.execute("DROP TABLE balanced")
        with self.connect() as worker, worker.cursor() as cur:
            actual = []
            for part in parts:
                cur.adbc_read_partition(part)
                table = cur.fetch_arrow_table()
                self.assertEqual(table.schema, schema)
                self.assertEqual(table.num_rows, 250)
                actual.extend(zip(table["id"].to_pylist(), table["exact"].to_pylist(),
                                  table["payload"].to_pylist()))
            self.assertEqual(sorted(actual), [
                (i, 9007199254740993 + i, None if i%7 == 0 else f"row{i}")
                for i in range(5000, 6000)
            ])
            with self.conn.cursor() as owner:
                owner.execute(f"DROP PARTITION SNAPSHOT '{group}'")
            with self.assertRaises(adbc_driver_manager.Error):
                cur.adbc_read_partition(parts[0])

    def test_live_partition_visibility(self):
        with self.conn.cursor() as cur:
            self.execute_retry(cur, "CREATE TABLE t(id INTEGER,v INTEGER)")
            self.execute_retry(cur, "INSERT INTO t VALUES (1,10),(2,20),(3,30),(4,40)")
            self.execute_retry(cur, "UPDATE t SET v=99 WHERE id=2")
            self.execute_retry(cur, "DELETE FROM t WHERE id=3")
            cur.adbc_statement.set_options(**{"adbc.redis.partition_count": "3"})
            parts, schema = cur.adbc_execute_partitions("SELECT id,v FROM t WHERE v>15")
            group = cur.adbc_statement.get_option("adbc.redis.partition_snapshot_id")
            self.assertEqual(len(group), 32)
            self.assertEqual(schema.names, ["id", "v"])
            self.execute_retry(cur, "UPDATE t SET v=88 WHERE id=4")
            rows = []
            for part in parts:
                cur.adbc_read_partition(part)
                rows.extend(cur.fetchall())
            self.assertEqual(sorted(rows), [(2, 99), (4, 40)])
            # The producer generation also survives replacement of the table.
            self.execute_retry(cur, "TRUNCATE t")
            rows = []
            for part in parts:
                cur.adbc_read_partition(part)
                rows.extend(cur.fetchall())
            self.assertEqual(sorted(rows), [(2, 99), (4, 40)])
            self.execute_retry(cur, f"DROP PARTITION SNAPSHOT '{group}'")

    def test_live_partition_survives_producer_close_and_recreation(self):
        producer = self.connect()
        with producer.cursor() as cur:
            self.execute_retry(cur, "CREATE TABLE t(id BIGINT,v BIGINT)")
            self.execute_retry(cur, "INSERT INTO t VALUES (1,9007199254740993),(2,42)")
            parts, schema = cur.adbc_execute_partitions("SELECT id,v FROM t")
            group = cur.adbc_statement.get_option("adbc.redis.partition_snapshot_id")
        producer.close()
        with self.conn.cursor() as cur:
            self.execute_retry(cur, "DROP TABLE t")
            self.execute_retry(cur, "CREATE TABLE t(replacement VARCHAR)")
            self.execute_retry(cur, "INSERT INTO t VALUES ('new')")
            rows = []
            for part in parts:
                cur.adbc_read_partition(part)
                self.assertEqual(cur.description[0][0], schema.names[0])
                rows.extend(cur.fetchall())
            self.assertEqual(sorted(rows), [(1, 9007199254740993), (2, 42)])
            self.execute_retry(cur, f"DROP PARTITION SNAPSHOT '{group}'")
            with self.assertRaises(adbc_driver_manager.Error):
                cur.adbc_read_partition(parts[0])

    def test_retained_partition_replay(self):
        retained = self.connect(**{
            "adbc.redis.partition_result_namespace": "snapshot_native_compat",
            # The owned Software fixture's setup verified noeviction via API.
            "adbc.redis.partition_control_persistence": "guaranteed",
        })
        self.addCleanup(retained.close)
        with retained.cursor() as cur:
            self.execute_retry(cur, "CREATE TABLE t(id BIGINT,v INTEGER)")
            self.execute_retry(cur, "INSERT INTO t VALUES (1,10),(2,20),(3,30)")
            self.execute_retry(cur, "UPDATE t SET v=99 WHERE id=2")
            self.execute_retry(cur, "DELETE FROM t WHERE id=1")
            cur.adbc_statement.set_options(**{"adbc.redis.partition_result_mode": "retained"})
            parts, schema = cur.adbc_execute_partitions("SELECT id FROM t WHERE v>=30 ORDER BY id")
            self.assertEqual(schema.names, ["id"])
            self.execute_retry(cur, "DROP TABLE t")
            rows = []
            for part in parts:
                cur.adbc_read_partition(part)
                rows.extend(cur.fetchall())
            self.assertEqual(rows, [(2,), (3,)])


if __name__ == "__main__":
    unittest.main(verbosity=2)
