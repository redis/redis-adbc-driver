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

"""ALTER TABLE … ALTER COLUMN … TYPE (#114), through the C API:
AdbcConnectionGetTableSchema's types follow the change, the rows read the
converted values, and a column that doesn't convert without USING fails with
Postgres's error, leaving the table as it was."""

import adbc_driver_manager
import pyarrow
import pytest

TABLE = "it_alter_type"


@pytest.fixture
def table(conn):
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {TABLE}")
        cur.execute(f"CREATE TABLE {TABLE} (id INTEGER, c TEXT, v VARCHAR(5))")
        cur.execute(f"INSERT INTO {TABLE} VALUES (1, '10', 'a'), (2, '9', 'bb')")
    yield TABLE
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE {TABLE}")


def types(conn) -> dict[str, pyarrow.DataType]:
    return {f.name: f.type for f in conn.adbc_get_table_schema(TABLE)}


def test_alter_type(conn, table) -> None:
    assert types(conn) == {"id": pyarrow.int32(), "c": pyarrow.string(), "v": pyarrow.string()}
    with conn.cursor() as cur:
        with pytest.raises(adbc_driver_manager.ProgrammingError) as e:
            cur.execute(f"ALTER TABLE {TABLE} ALTER COLUMN c TYPE INTEGER")
        assert (
            'column "c" cannot be cast automatically to type integer;'
            ' you might need to specify "USING c::integer"'
        ) in str(e.value)
        assert e.value.sqlstate == "42804"
    assert types(conn)["c"] == pyarrow.string()

    with conn.cursor() as cur:
        cur.execute(f"ALTER TABLE {TABLE} ALTER COLUMN c TYPE BIGINT USING c::BIGINT, ALTER COLUMN id SET DATA TYPE NUMERIC(6,2)")
    assert types(conn) == {"id": pyarrow.decimal128(6, 2), "c": pyarrow.int64(), "v": pyarrow.string()}
    with conn.cursor() as cur:
        cur.execute(f"SELECT c, v FROM {TABLE} WHERE c > 5 ORDER BY c")
        assert cur.fetchall() == [(9, "bb"), (10, "a")]
