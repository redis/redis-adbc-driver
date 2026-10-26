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

"""ALTER TABLE … ALTER COLUMN … SET NOT NULL / DROP NOT NULL (#114), through
the C API: AdbcConnectionGetTableSchema's nullable flags follow the change,
and SET NOT NULL on a column with a NULL fails with Postgres's error."""

import adbc_driver_manager
import pytest

TABLE = "it_alter_not_null"


@pytest.fixture
def table(conn):
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {TABLE}")
        cur.execute(f"CREATE TABLE {TABLE} (id INTEGER NOT NULL, v INTEGER)")
        cur.execute(f"INSERT INTO {TABLE} VALUES (1, 10), (2, NULL)")
    yield TABLE
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE {TABLE}")


def nullable(conn) -> dict[str, bool]:
    return {f.name: f.nullable for f in conn.adbc_get_table_schema(TABLE)}


def test_set_and_drop_not_null(conn, table) -> None:
    assert nullable(conn) == {"id": False, "v": True}
    with conn.cursor() as cur:
        with pytest.raises(adbc_driver_manager.IntegrityError) as e:
            cur.execute(f"ALTER TABLE {TABLE} ALTER COLUMN v SET NOT NULL")
        assert f'column "v" of relation "{TABLE}" contains null values' in str(e.value)
    assert nullable(conn) == {"id": False, "v": True}

    with conn.cursor() as cur:
        cur.execute(f"UPDATE {TABLE} SET v = 0 WHERE v IS NULL")
        cur.execute(f"ALTER TABLE {TABLE} ALTER COLUMN v SET NOT NULL, ALTER COLUMN id DROP NOT NULL")
    assert nullable(conn) == {"id": True, "v": False}
    with conn.cursor() as cur:
        with pytest.raises(adbc_driver_manager.IntegrityError):
            cur.execute(f"INSERT INTO {TABLE} (id) VALUES (3)")
        cur.execute(f"INSERT INTO {TABLE} (v) VALUES (3)")
        cur.execute(f"SELECT id, v FROM {TABLE} ORDER BY v")
        assert cur.fetchall() == [(2, 0), (None, 3), (1, 10)]
