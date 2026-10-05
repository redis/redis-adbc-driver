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

"""Bulk ingest in a session time zone (#151), through the C API: text
without an offset, and Arrow timestamps without a zone, written to a
TIMESTAMP WITH TIME ZONE column are local times, as an INSERT reads them."""

import datetime as dt

import adbc_driver_manager.dbapi
import pyarrow
import pytest

TABLE = "it_ingest_tz"


@pytest.fixture
def ny_conn(driver_path, db_kwargs):
    kwargs = {**db_kwargs, "adbc.redis.time_zone": "America/New_York"}
    with adbc_driver_manager.dbapi.connect(driver=driver_path, db_kwargs=kwargs, autocommit=True) as conn:
        with conn.cursor() as cur:
            cur.execute(f"DROP TABLE IF EXISTS {TABLE}")
            cur.execute(f"CREATE TABLE {TABLE} (src VARCHAR, ts TIMESTAMP WITH TIME ZONE)")
        yield conn
        with conn.cursor() as cur:
            cur.execute(f"DROP TABLE {TABLE}")


def local_times(conn) -> dict[str, str]:
    with conn.cursor() as cur:
        cur.execute(f"SELECT src, CAST(ts AS VARCHAR) FROM {TABLE}")
        return dict(cur.fetchall())


def test_ingest_reads_local_times(ny_conn) -> None:
    with ny_conn.cursor() as cur:
        cur.execute(f"INSERT INTO {TABLE} VALUES ('INSERT of text', '2024-01-10 02:00:00')")
        for src, ts in [
            ("Arrow string", pyarrow.array(["2024-01-10 02:00:00"])),
            ("Arrow string with an offset", pyarrow.array(["2024-01-10 07:00:00+00"])),
            ("naive Arrow timestamp", pyarrow.array([dt.datetime(2024, 1, 10, 2)], pyarrow.timestamp("us"))),
            (
                "Arrow timestamp tz=UTC",
                pyarrow.array([dt.datetime(2024, 1, 10, 7, tzinfo=dt.timezone.utc)], pyarrow.timestamp("us", tz="UTC")),
            ),
        ]:
            cur.adbc_ingest(TABLE, pyarrow.table({"src": [src], "ts": ts}), mode="append")
    assert local_times(ny_conn) == {
        "INSERT of text": "2024-01-10 02:00:00-05",
        "Arrow string": "2024-01-10 02:00:00-05",
        "Arrow string with an offset": "2024-01-10 02:00:00-05",
        "naive Arrow timestamp": "2024-01-10 02:00:00-05",
        "Arrow timestamp tz=UTC": "2024-01-10 02:00:00-05",
    }


def test_ingest_daylight_saving_time(ny_conn) -> None:
    """A local time that the spring-forward transition skips has the offset
    before it, and one that the fall-back transition repeats the offset
    after it, as an INSERT reads them."""
    with ny_conn.cursor() as cur:
        cur.execute(
            f"INSERT INTO {TABLE} VALUES ('INSERT skipped', '2024-03-10 02:30'), ('INSERT repeated', '2024-11-03 01:30')"
        )
        cur.adbc_ingest(
            TABLE,
            pyarrow.table({"src": ["text skipped", "text repeated"], "ts": ["2024-03-10 02:30", "2024-11-03 01:30"]}),
            mode="append",
        )
        cur.adbc_ingest(
            TABLE,
            pyarrow.table(
                {
                    "src": ["naive skipped", "naive repeated"],
                    "ts": pyarrow.array(
                        [dt.datetime(2024, 3, 10, 2, 30), dt.datetime(2024, 11, 3, 1, 30)], pyarrow.timestamp("us")
                    ),
                }
            ),
            mode="append",
        )
    got = local_times(ny_conn)
    for src in ["INSERT", "text", "naive"]:
        assert got[f"{src} skipped"] == "2024-03-10 03:30:00-04", src
        assert got[f"{src} repeated"] == "2024-11-03 01:30:00-05", src
