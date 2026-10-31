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

"""Rows as values through the C API: a record is an Arrow struct whose
children are named f1, f2, … (as Postgres names an anonymous record's
fields), each nullable; a NULL record is a NULL struct."""

import decimal

import adbc_driver_manager.dbapi
import pyarrow
import pytest

TABLE = "it_row_values"


@pytest.fixture
def rows_conn(driver_path, db_kwargs):
    with adbc_driver_manager.dbapi.connect(driver=driver_path, db_kwargs=db_kwargs, autocommit=True) as conn:
        with conn.cursor() as cur:
            cur.execute(f"DROP TABLE IF EXISTS {TABLE}")
            cur.execute(f"CREATE TABLE {TABLE} (id INTEGER, a INTEGER, b VARCHAR)")
            cur.execute(f"INSERT INTO {TABLE} VALUES (1, 1, 'x'), (2, 1, NULL), (3, NULL, 'y'), (4, NULL, NULL)")
        yield conn
        with conn.cursor() as cur:
            cur.execute(f"DROP TABLE {TABLE}")


def test_row_is_struct(rows_conn) -> None:
    with rows_conn.cursor() as cur:
        cur.execute("SELECT ROW(1, 'x'), ((1, 2.5), NULL) AS n, ROW() AS e")
        table = cur.fetch_arrow_table()
    assert table.schema == pyarrow.schema(
        [
            ("row", pyarrow.struct([("f1", pyarrow.int32()), ("f2", pyarrow.string())])),
            (
                "n",
                pyarrow.struct(
                    [
                        ("f1", pyarrow.struct([("f1", pyarrow.int32()), ("f2", pyarrow.decimal128(2, 1))])),
                        ("f2", pyarrow.null()),
                    ]
                ),
            ),
            ("e", pyarrow.struct([])),
        ]
    )
    assert table.to_pylist() == [
        {"row": {"f1": 1, "f2": "x"}, "n": {"f1": {"f1": 1, "f2": decimal.Decimal("2.5")}, "f2": None}, "e": {}}
    ]


def test_records_of_a_table(rows_conn) -> None:
    """ORDER BY a record sorts NULL fields last, and DESC reverses it; a
    record of NULL fields is not a NULL record."""
    with rows_conn.cursor() as cur:
        cur.execute(f"SELECT id, (a, b) AS r, CASE WHEN id < 4 THEN (b, a) END AS c FROM {TABLE} ORDER BY (a, b) DESC")
        table = cur.fetch_arrow_table()
    assert table.schema.field("r").type == pyarrow.struct([("f1", pyarrow.int32()), ("f2", pyarrow.string())])
    assert table.schema.field("c").type == pyarrow.struct([("f1", pyarrow.string()), ("f2", pyarrow.int32())])
    assert table.to_pylist() == [
        {"id": 4, "r": {"f1": None, "f2": None}, "c": None},
        {"id": 3, "r": {"f1": None, "f2": "y"}, "c": {"f1": "y", "f2": None}},
        {"id": 2, "r": {"f1": 1, "f2": None}, "c": {"f1": None, "f2": 1}},
        {"id": 1, "r": {"f1": 1, "f2": "x"}, "c": {"f1": "x", "f2": 1}},
    ]
    assert table.column("c").null_count == 1
    assert table.column("r").null_count == 0


def test_record_text_and_schema(rows_conn) -> None:
    with rows_conn.cursor() as cur:
        cur.execute(f"SELECT (a, b)::text FROM {TABLE} ORDER BY id")
        assert [r[0] for r in cur.fetchall()] == ["(1,x)", "(1,)", "(,y)", "(,)"]
        schema = cur.adbc_execute_schema(f"SELECT (a, b) FROM {TABLE}")
    assert schema == pyarrow.schema([("row", pyarrow.struct([("f1", pyarrow.int32()), ("f2", pyarrow.string())]))])


def test_anonymous_record_restrictions(rows_conn) -> None:
    with rows_conn.cursor() as cur:
        with pytest.raises(adbc_driver_manager.ProgrammingError, match='column "row" has pseudo-type record') as e:
            cur.execute(f"CREATE TABLE {TABLE}_c AS SELECT (a, b) FROM {TABLE}")
        assert e.value.sqlstate == "42P16", e.value
        with pytest.raises(adbc_driver_manager.ProgrammingError, match=r"function upper\(record\) does not exist") as e:
            cur.execute(f"SELECT upper((a, b)) FROM {TABLE}")
        assert e.value.sqlstate == "42883", e.value


def test_named_struct_can_be_stored(rows_conn) -> None:
    source = pyarrow.table({"r": pyarrow.array(
        [{"f1": 1}, None, {"f1": None}], pyarrow.struct([("f1", pyarrow.int64())]))})
    with rows_conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {TABLE}_i")
        try:
            cur.adbc_ingest(f"{TABLE}_i", source)
            cur.execute(f"SELECT r FROM {TABLE}_i ORDER BY __rowid")
            assert cur.fetch_arrow_table().equals(source)
            assert rows_conn.adbc_get_table_schema(f"{TABLE}_i").equals(source.schema)
        finally:
            cur.execute(f"DROP TABLE IF EXISTS {TABLE}_i")
