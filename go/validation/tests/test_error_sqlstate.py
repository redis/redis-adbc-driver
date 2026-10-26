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

"""Errors' SQLSTATEs and statuses (see go/sqlstate.go), through the C API:
exc.sqlstate is Postgres 16's code for the same error, and the status (and
so the DB-API exception class) follows from it, as in
adbc-driver-postgresql."""

import adbc_driver_manager
import pytest

TABLE = "it_err_py"
STREAMED = "it_err_py_stream"
Status = adbc_driver_manager.AdbcStatusCode


@pytest.fixture
def tables(conn):
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {TABLE}")
        cur.execute(f"DROP TABLE IF EXISTS {STREAMED}")
        cur.execute(f"CREATE TABLE {TABLE} (id INTEGER NOT NULL, n INTEGER, v VARCHAR(3), c INTEGER CHECK (c > 0))")
        # A text column whose last row isn't a number: the error comes while
        # the result is read.
        cur.execute(
            f"CREATE TABLE {STREAMED} AS SELECT g,"
            " CASE WHEN g = 5000 THEN 'x' ELSE CAST(g AS VARCHAR) END AS s"
            " FROM generate_series(1, 5000) AS t(g)"
        )
    yield
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE {TABLE}")
        cur.execute(f"DROP TABLE {STREAMED}")


@pytest.mark.parametrize(
    "sql,exc,status,sqlstate",
    [
        ("SELECT CAST('abc' AS INTEGER)", adbc_driver_manager.DataError, Status.INVALID_DATA, "22P02"),
        ("SELECT CAST(3000000000 AS INTEGER)", adbc_driver_manager.DataError, Status.INVALID_DATA, "22003"),
        ("SELECT 1 / 0", adbc_driver_manager.DataError, Status.INVALID_DATA, "22012"),
        ("SELECT CAST('abc' AS DATE)", adbc_driver_manager.DataError, Status.INVALID_DATA, "22007"),
        ("SELECT CAST('2024-02-30' AS DATE)", adbc_driver_manager.DataError, Status.INVALID_DATA, "22008"),
        (f"INSERT INTO {TABLE} (id, n) VALUES (1, 'x')", adbc_driver_manager.DataError, Status.INVALID_DATA, "22P02"),
        (f"INSERT INTO {TABLE} (id, v) VALUES (1, 'abcd')", adbc_driver_manager.DataError, Status.INVALID_DATA, "22001"),
        (f"INSERT INTO {TABLE} (id) VALUES (NULL)", adbc_driver_manager.IntegrityError, Status.INTEGRITY, "23502"),
        (f"INSERT INTO {TABLE} (id, c) VALUES (1, -1)", adbc_driver_manager.IntegrityError, Status.INTEGRITY, "23514"),
        ("SELECT CAST(DATE '2024-01-01' AS BOOLEAN)", adbc_driver_manager.ProgrammingError, Status.INVALID_ARGUMENT, "42846"),
        ("SELECT 1 +", adbc_driver_manager.ProgrammingError, Status.INVALID_ARGUMENT, "42601"),
        (f"SELECT nosuch FROM {TABLE}", adbc_driver_manager.ProgrammingError, Status.NOT_FOUND, "42703"),
        ("SELECT * FROM it_err_py_nosuch", adbc_driver_manager.ProgrammingError, Status.NOT_FOUND, "42P01"),
        ("SELECT nosuchfunc(1)", adbc_driver_manager.ProgrammingError, Status.NOT_FOUND, "42883"),
        (f"CREATE TABLE {TABLE} (x INTEGER)", adbc_driver_manager.ProgrammingError, Status.ALREADY_EXISTS, "42P07"),
    ],
)
def test_error_sqlstate(conn, tables, sql, exc, status, sqlstate) -> None:
    with conn.cursor() as cur:
        with pytest.raises(exc) as e:
            cur.execute(sql)
            cur.fetchall()
        assert e.value.sqlstate == sqlstate, e.value
        assert e.value.status_code == status, e.value


def test_feature_not_supported(conn) -> None:
    # NOT_IMPLEMENTED is NotSupportedError, as in adbc-driver-postgresql.
    with conn.cursor() as cur:
        with pytest.raises(adbc_driver_manager.NotSupportedError) as e:
            cur.execute("SELECT DATE_BIN(INTERVAL '1 month', TIMESTAMP '2024-01-01', TIMESTAMP '2024-01-01')")
        assert e.value.sqlstate == "0A000", e.value


def test_streamed_error_sqlstate(conn, tables) -> None:
    # The error of a row read from the stream (AdbcErrorFromArrayStream).
    with conn.cursor() as cur:
        with pytest.raises(adbc_driver_manager.DataError) as e:
            cur.execute(f"SELECT g, CAST(s AS INTEGER) FROM {STREAMED}")
            cur.fetch_arrow_table()
        assert e.value.sqlstate == "22P02", e.value
        assert e.value.status_code == Status.INVALID_DATA, e.value

        # The statement runs again.
        cur.execute(f"SELECT count(*) FROM {STREAMED}")
        assert cur.fetchall() == [(5000,)]
