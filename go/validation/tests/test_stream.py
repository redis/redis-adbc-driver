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

"""Streamed results (see go/stream.go), through the C API."""

import threading

import adbc_driver_manager
import pyarrow
import pytest
import redis

from .redis_client import raw_options

TABLE = "it_cancel_t"
ROWS = 25_000  # three cursor pages of 10,000 rows
BATCH = 1000


def _pairs(reply):
    """A RESP2 list of keys and values, or a RESP3 map, as a dict."""
    return reply if isinstance(reply, dict) else dict(zip(reply[::2], reply[1::2]))


def index_of(conn, table: str) -> str:
    """The table's index, whose name changes when it is created again."""
    with conn.cursor() as cur:
        cur.execute(
            "SELECT index_name FROM information_schema.tables"
            f" WHERE table_schema = 'public' AND table_name = '{table}'"
        )
        return cur.fetchone()[0]


def open_cursors(db_kwargs, index: str) -> int:
    """The open cursors FT.INFO reports, on the node that runs the index's
    searches: all of them (global_total), as a cluster's FT.INFO doesn't
    count the index's own (index_total)."""
    uri, options = db_kwargs["uri"], raw_options(db_kwargs)
    client = redis.Redis.from_url(uri, **options)
    try:
        if not client.info("cluster").get("cluster_enabled"):
            return _pairs(_pairs(client.execute_command("FT.INFO", index))["cursor_stats"])["global_total"]
    finally:
        client.close()
    cluster = redis.RedisCluster.from_url(uri, **options)
    try:
        node = cluster.get_node_from_key(index)
        reply = cluster.execute_command("FT.INFO", index, target_nodes=node)
        return _pairs(_pairs(reply)["cursor_stats"])["global_total"]
    finally:
        cluster.close()


def aggregate_cap(db_kwargs, index: str) -> int | None:
    """Optional test introspection; CONFIG is not required of driver readers."""
    uri, options = db_kwargs["uri"], raw_options(db_kwargs)
    client = redis.Redis.from_url(uri, **options)
    try:
        clustered = client.info("cluster").get("cluster_enabled")
        if not clustered:
            return int(client.config_get("search-max-aggregate-results")["search-max-aggregate-results"])
    except (redis.RedisError, KeyError, ValueError):
        return None
    finally:
        client.close()
    cluster = redis.RedisCluster.from_url(uri, **options)
    try:
        reply = cluster.execute_command("CONFIG", "GET", "search-max-aggregate-results",
                                        target_nodes=cluster.get_node_from_key(index))
        return int(_pairs(reply)["search-max-aggregate-results"])
    except (redis.RedisError, KeyError, ValueError):
        return None
    finally:
        cluster.close()


@pytest.fixture
def table(conn):
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {TABLE}")
        cur.adbc_ingest(TABLE, pyarrow.table({"id": pyarrow.array(range(ROWS), pyarrow.int64())}))
    yield TABLE
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE {TABLE}")


def test_cancel_from_another_thread(conn, db_kwargs, table) -> None:
    index = index_of(conn, table)
    before = open_cursors(db_kwargs, index)
    with conn.cursor() as cur:
        cur.adbc_statement.set_options(**{"adbc.redis.stream_batch_rows": str(BATCH)})
        cur.execute(f"SELECT id FROM {table}")
        assert cur.rowcount == -1, "the result wasn't streamed"
        started, cancelled = threading.Event(), threading.Event()
        rows, errors = [], []

        def read() -> None:
            try:
                rows.extend(cur.fetchmany(BATCH))  # the first batch
                started.set()
                cancelled.wait(30)
                rows.extend(cur.fetchall())
            except Exception as e:
                errors.append(e)
            finally:
                started.set()

        reader = threading.Thread(target=read)
        reader.start()
        assert started.wait(30)
        # This single-column scan requests 10,000 rows per cursor page. A
        # cap-bounded window smaller than that is already buffered; cancellation
        # must still stop its logical reader and prevent opening later windows.
        cap = aggregate_cap(db_kwargs, index)
        active = open_cursors(db_kwargs, index)
        if cap is not None and 0 < cap < 10_000:
            assert active == before
        elif cap == 10_000:  # exact page boundary may need one terminal READ
            assert before <= active <= before + 1
        else:
            assert active == before + 1
        cur.adbc_cancel()
        cancelled.set()
        reader.join(30)
        assert not reader.is_alive()

        assert len(rows) == BATCH
        assert len(errors) == 1, errors
        assert isinstance(errors[0], adbc_driver_manager.OperationalError), errors[0]
        assert errors[0].status_code == adbc_driver_manager.AdbcStatusCode.CANCELLED
        assert errors[0].sqlstate == "57014", errors[0]
        assert open_cursors(db_kwargs, index) == before

        # Nothing is left to cancel.
        with pytest.raises(adbc_driver_manager.ProgrammingError) as e:
            cur.adbc_cancel()
        assert e.value.status_code == adbc_driver_manager.AdbcStatusCode.INVALID_STATE

        # The statement runs again.
        cur.execute(f"SELECT id FROM {table}")
        assert len(cur.fetchall()) == ROWS
