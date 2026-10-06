# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Closing a connection while a rename with adbc.redis.rename_rekey moves
the rows on another thread (see stopRekeys in go/rekey.go), through the C
API: the rename is rolled back before close() returns, and leaves no lease,
so other connections change the table at once."""

import json
import threading
import time

import adbc_driver_manager.dbapi
import pyarrow
import pytest
import redis

TABLE = "it_close_rekey"
ROWS = 100_000
REKEY = "adbc:{meta}:rekey"


def raw_client(uri: str):
    client = redis.Redis.from_url(uri, decode_responses=True)
    if client.info("cluster").get("cluster_enabled"):
        client.close()
        return redis.RedisCluster.from_url(uri, decode_responses=True)
    return client


@pytest.fixture
def table(conn):
    with conn.cursor() as cur:
        for t in (TABLE, TABLE + "2"):
            cur.execute(f"DROP TABLE IF EXISTS {t}")
        cur.adbc_ingest(TABLE, pyarrow.table({"id": pyarrow.array(range(ROWS), pyarrow.int64())}))
    yield TABLE
    with conn.cursor() as cur:
        for t in (TABLE, TABLE + "2"):
            cur.execute(f"DROP TABLE IF EXISTS {t}")


def test_close_during_rename(conn, driver_path, db_kwargs, table) -> None:
    client = raw_client(db_kwargs["uri"])
    try:
        renaming = adbc_driver_manager.dbapi.connect(
            driver=driver_path,
            db_kwargs={**db_kwargs, "adbc.redis.rename_rekey": "true"},
            autocommit=True,
        )
        cur = renaming.cursor()
        errors = []

        def rename() -> None:
            try:
                cur.execute(f"ALTER TABLE {table} RENAME TO {table}2")
            except Exception as e:
                errors.append(e)

        thread = threading.Thread(target=rename)
        thread.start()
        # Close once the rename has begun.
        jobs, deadline = {}, time.monotonic() + 30
        while not jobs and time.monotonic() < deadline:
            jobs = client.hgetall(REKEY)
            time.sleep(0.005)
        assert len(jobs) == 1, jobs
        owner = json.loads(next(iter(jobs.values())))["owner"]
        began = time.monotonic()
        renaming.close()  # releases the cursor's statement, then the connection
        took = time.monotonic() - began
        thread.join(30)
        assert not thread.is_alive()

        assert len(errors) == 1, errors
        assert "the rename was" in str(errors[0]), errors[0]
        assert took < 5, f"close() took {took:.1f}s"
        # Rolled back, with no lease left.
        assert client.hlen(REKEY) == 0
        assert client.exists(f"adbc:{{meta}}:rekey:alive:{owner}") == 0
    finally:
        client.close()

    # The table is as it was, and changes at once.
    with conn.cursor() as cur:
        began = time.monotonic()
        cur.execute(f"INSERT INTO {table} VALUES ({ROWS})")
        assert time.monotonic() - began < 5
        cur.execute(f"SELECT count(*) FROM {table}")
        assert cur.fetchone() == (ROWS + 1,)
