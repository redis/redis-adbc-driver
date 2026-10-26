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

"""Cancelling a statement while it executes (see go/cancel.go), through the
C API: the work the driver does in memory stops, with CANCELLED."""

import threading
import time

import adbc_driver_manager
import pytest

TABLE = "it_cancel_join"
CANCEL_AFTER = 1.0  # seconds
LAG = 1.0  # how soon after the cancel the statement must fail


@pytest.fixture
def table(conn):
    """30,000 rows in 97 groups: its self-join has 9.3M pairs."""
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {TABLE}")
        cur.execute(f"CREATE TABLE {TABLE} AS SELECT g, g % 97 AS k FROM generate_series(1, 30000) AS s(g)")
    yield TABLE
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE {TABLE}")


@pytest.mark.parametrize(
    "sql",
    [
        # Rows the driver makes and sums: about 8 seconds uncancelled.
        "SELECT sum(length(repeat(cast(g AS varchar), 2000))) FROM generate_series(1, 1000000) AS s(g)",
        # A join the driver does: about 18 seconds uncancelled.
        f"SELECT count(*) FROM {TABLE} a JOIN {TABLE} b ON a.k = b.k WHERE a.g + b.g > 0",
    ],
    ids=["aggregate", "join"],
)
def test_cancel_from_another_thread(conn, table, sql) -> None:
    with conn.cursor() as cur:
        cancelled_at, errors = [], []

        def cancel() -> None:
            cancelled_at.append(time.monotonic())
            try:
                cur.adbc_cancel()
            except Exception as e:
                errors.append(e)

        timer = threading.Timer(CANCEL_AFTER, cancel)
        timer.start()
        try:
            with pytest.raises(adbc_driver_manager.OperationalError) as e:
                cur.execute(sql)
            failed_at = time.monotonic()
        finally:
            timer.cancel()
            timer.join()
        assert e.value.status_code == adbc_driver_manager.AdbcStatusCode.CANCELLED, e.value
        assert e.value.sqlstate == "57014", e.value
        assert not errors, errors
        assert cancelled_at, "the statement failed before the cancel"
        assert failed_at - cancelled_at[0] < LAG, f"failed {failed_at - cancelled_at[0]:.1f}s after the cancel"

        # The statement runs again.
        cur.execute("SELECT 1")
        assert cur.fetchall() == [(1,)]
