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

"""Close through the C API during a private snapshot rekey copy.

The unpublished copy must not become visible, and Close must release its owner
before returning so another connection can immediately change the original.
"""

import json
import threading
import time

import adbc_driver_manager.dbapi
import pyarrow
import pytest
import redis

from .redis_client import raw_options

TABLE = "it_close_rekey"
ROWS = 100_000
CATALOG = "adbc:{meta}:snapshot:catalog"


def raw_client(db_kwargs):
    uri = db_kwargs["uri"]
    options = raw_options(db_kwargs)
    client = redis.Redis.from_url(uri, **options)
    if client.info("cluster").get("cluster_enabled"):
        client.close()
        return redis.RedisCluster.from_url(uri, **options)
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
    client = raw_client(db_kwargs)
    renaming, thread = None, None
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
        # Observe an acknowledged private row batch, rather than the legacy
        # mutable-rekey journal. No other statement writes in this interval.
        jobs, deadline = [], time.monotonic() + 30
        while not jobs and thread.is_alive() and time.monotonic() < deadline:
            jobs = [
                (field, work)
                for field, raw in client.hgetall(CATALOG).items()
                if field.startswith("w:")
                and (work := json.loads(raw)).get("state") == "staging"
                and work.get("row_batch", {}).get("rows", 0) > 0
            ]
            if not jobs:
                time.sleep(0.001)
        assert len(jobs) == 1, "rename did not expose an in-flight private copy"
        workspace, work = jobs[0]
        owner = work["token"]
        assert client.hget(CATALOG, "root") != workspace[2:]
        began = time.monotonic()
        renaming.close()  # releases the cursor's statement, then the connection
        took = time.monotonic() - began
        thread.join(30)
        assert not thread.is_alive()

        assert len(errors) == 1, errors
        assert isinstance(errors[0], adbc_driver_manager.dbapi.Error), errors[0]
        assert "closed" in str(errors[0]).lower() or "cancel" in str(errors[0]).lower(), errors[0]
        assert took < 5, f"close() took {took:.1f}s"
        # Unpublished and ownerless before Close returns. Private output may
        # remain for the reference-aware collector, but cannot publish.
        assert client.hget(CATALOG, "root") != workspace[2:]
        assert not client.hexists(CATALOG, f"p:{owner}")
        remaining = client.hget(CATALOG, workspace)
        assert remaining is None or json.loads(remaining)["state"] == "deleting"
    finally:
        try:
            if renaming is not None:
                renaming.close()
        finally:
            if thread is not None:
                thread.join(30)
            client.close()

    # The table is as it was, and changes at once.
    with conn.cursor() as cur:
        began = time.monotonic()
        cur.execute(f"INSERT INTO {table} VALUES ({ROWS})")
        assert time.monotonic() - began < 5
        cur.execute(f"SELECT count(*) FROM {table}")
        assert cur.fetchone() == (ROWS + 1,)
        cur.execute(f"SELECT id FROM {table} ORDER BY id")
        assert cur.fetchall() == [(i,) for i in range(ROWS + 1)]
