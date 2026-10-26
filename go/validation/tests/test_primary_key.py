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

"""PRIMARY KEY through the C API: AdbcConnectionGetTableSchema shows the
key's columns as not nullable, a NULL key fails with Postgres's error (also
in bulk ingest), and AdbcConnectionGetObjects lists the key until it is
dropped. Its uniqueness isn't enforced."""

import adbc_driver_manager
import pyarrow
import pytest

TABLE = "it_primary_key"


@pytest.fixture
def table(conn):
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {TABLE}")
        cur.execute(f"CREATE TABLE {TABLE} (a INTEGER, b VARCHAR, v INTEGER, PRIMARY KEY (b, a))")
    yield TABLE
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE {TABLE}")


def nullable(conn) -> dict[str, bool]:
    return {f.name: f.nullable for f in conn.adbc_get_table_schema(TABLE)}


def constraints(conn) -> list[tuple[str, str, list[str]]]:
    objects = conn.adbc_get_objects(depth="all", table_name_filter=TABLE).read_all().to_pylist()
    return [
        (c["constraint_name"], c["constraint_type"], c["constraint_column_names"])
        for catalog in objects
        for schema in catalog["catalog_db_schemas"]
        for t in schema["db_schema_tables"]
        if t["table_name"] == TABLE
        for c in t["table_constraints"]
    ]


def test_primary_key(conn, table) -> None:
    assert nullable(conn) == {"a": False, "b": False, "v": True}
    assert constraints(conn) == [(f"{TABLE}_pkey", "PRIMARY KEY", ["b", "a"])]
    with conn.cursor() as cur:
        with pytest.raises(adbc_driver_manager.IntegrityError) as e:
            cur.execute(f"INSERT INTO {TABLE} (b, v) VALUES ('x', 1)")
        assert f'null value in column "a" of relation "{TABLE}" violates not-null constraint' in str(e.value)
        data = pyarrow.table({"a": pyarrow.array([1, None], pyarrow.int32()), "b": ["x", "y"]})
        with pytest.raises(adbc_driver_manager.IntegrityError):
            cur.adbc_ingest(TABLE, data, mode="append")
        # Not enforced: two rows with the same key.
        cur.execute(f"INSERT INTO {TABLE} VALUES (1, 'x', 1), (1, 'x', 2)")
        cur.execute(f"SELECT COUNT(*) FROM {TABLE}")
        assert cur.fetchall() == [(2,)]

        cur.execute(f"ALTER TABLE {TABLE} DROP CONSTRAINT {TABLE}_pkey")
    assert constraints(conn) == []
    assert nullable(conn) == {"a": False, "b": False, "v": True}
