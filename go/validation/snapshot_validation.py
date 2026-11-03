# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0 (the "License");
# you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
"""Native C/ADBC snapshot validation on REDIS_SNAPSHOT_URI (DISPOSABLE).

This script clears the explicitly selected test database before and after each
test. Run separately from the ordinary validation suite; see docs/snapshots.md.
"""

import os
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
            cur.execute("SELECT v FROM redis_foreign.app_index")
            self.assertEqual(cur.fetchall(), [(10,)])
            self.raw.hset("app:1", "v", "99")
            cur.execute("SELECT v FROM redis_foreign.app_index")
            self.assertEqual(cur.fetchall(), [(10,)])
            foreign.adbc_connection.set_options(**{"adbc.redis.foreign_indexes_refresh": "true"})
            cur.execute("SELECT v FROM redis_foreign.app_index")
            self.assertEqual(cur.fetchall(), [(99,)])
            self.assertEqual(self.raw.hget("app:1", "v"), "99")
            self.raw.execute_command("FT.INFO", "app_index")

    def test_live_partition_visibility(self):
        with self.conn.cursor() as cur:
            self.execute_retry(cur, "CREATE TABLE t(id INTEGER,v INTEGER)")
            self.execute_retry(cur, "INSERT INTO t VALUES (1,10),(2,20),(3,30),(4,40)")
            self.execute_retry(cur, "UPDATE t SET v=99 WHERE id=2")
            self.execute_retry(cur, "DELETE FROM t WHERE id=3")
            cur.adbc_statement.set_options(**{"adbc.redis.partition_count": "3"})
            parts, schema = cur.adbc_execute_partitions("SELECT id,v FROM t WHERE v>15")
            self.assertEqual(schema.names, ["id", "v"])
            self.execute_retry(cur, "UPDATE t SET v=88 WHERE id=4")
            rows = []
            for part in parts:
                cur.adbc_read_partition(part)
                rows.extend(cur.fetchall())
            self.assertEqual(sorted(rows), [(2, 99), (4, 88)])

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
