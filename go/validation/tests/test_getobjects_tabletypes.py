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

"""GetObjects type filters through the driver manager and native Arrow C stream."""

import uuid

import pytest


def _tables(
    conn, *, catalog="redis", schema=None, name=None, types=None,
    column=None, depth="tables",
):
    with conn.adbc_get_objects(
        depth=depth,
        catalog_filter=catalog,
        db_schema_filter=schema,
        table_name_filter=name,
        table_types_filter=types,
        column_name_filter=column,
    ) as reader:
        objects = reader.read_all().to_pylist()
    return {
        (cat["catalog_name"], sch["db_schema_name"], table["table_name"]): (
            table["table_type"],
            None
            if table["table_columns"] is None
            else [col["column_name"] for col in table["table_columns"]],
        )
        for cat in objects
        for sch in cat["catalog_db_schemas"]
        for table in sch["db_schema_tables"]
    }


@pytest.fixture
def tabletype_objects(conn):
    schema = "it_tabletypes_" + uuid.uuid4().hex[:12]
    other = schema + "_other"
    with conn.cursor() as cur:
        cur.execute(f"CREATE SCHEMA {schema}")
        cur.execute(f"CREATE SCHEMA {other}")
        try:
            cur.execute(f"CREATE TABLE {schema}.kept_t (value INTEGER, label TEXT)")
            cur.execute(f"INSERT INTO {schema}.kept_t VALUES (1, 'one')")
            cur.execute(f"CREATE VIEW {schema}.kept_v AS SELECT * FROM {schema}.kept_t")
            cur.execute(
                f"CREATE MATERIALIZED VIEW {schema}.kept_mv AS SELECT * FROM {schema}.kept_t"
            )
            cur.execute(f"CREATE TABLE {schema}.excluded_t (other INTEGER)")
            cur.execute(f"CREATE TABLE {other}.kept_t (other INTEGER)")
            yield schema, other
        finally:
            cur.execute(f"DROP SCHEMA IF EXISTS {other} CASCADE")
            cur.execute(f"DROP SCHEMA IF EXISTS {schema} CASCADE")


def test_get_objects_type_filter_matrix(conn, tabletype_objects):
    schema, _ = tabletype_objects
    expected_types = {
        "kept_t": "TABLE", "kept_v": "VIEW", "kept_mv": "MATERIALIZED VIEW",
    }
    # Python driver-manager 1.13 normalizes both None and [] to C NULL:
    # both request no filtering. Empty/unknown type strings are actual filters.
    cases = [
        (None, expected_types),
        ([], expected_types),
        (["TABLE"], {"kept_t": "TABLE"}),
        (["VIEW"], {"kept_v": "VIEW"}),
        (["MATERIALIZED VIEW"], {"kept_mv": "MATERIALIZED VIEW"}),
        (["VIEW", "TABLE"], {"kept_t": "TABLE", "kept_v": "VIEW"}),
        (["TABLE", "MATERIALIZED VIEW"], {"kept_t": "TABLE", "kept_mv": "MATERIALIZED VIEW"}),
        (["TABLE", "TABLE"], {"kept_t": "TABLE"}),
        (["UNKNOWN", "VIEW"], {"kept_v": "VIEW"}),
        (["UNKNOWN"], {}),
        ([""], {}),
        (["table"], {}),
    ]
    for types, expected in cases:
        got = _tables(conn, schema=schema, name="kept%", types=types)
        assert got == {
            ("redis", schema, name): (kind, None) for name, kind in expected.items()
        }, types


def test_get_objects_type_filter_composes_with_discovery_filters(conn, tabletype_objects):
    schema, other = tabletype_objects
    assert _tables(conn, schema=schema, name="kept_v", types=["TABLE"]) == {}
    assert _tables(conn, schema=schema, name="kept_v", types=["VIEW"]) == {
        ("redis", schema, "kept_v"): ("VIEW", None),
    }
    for filters in [
        {"catalog": "missing_catalog", "schema": schema},
        {"catalog": "", "schema": schema},
        {"schema": schema + "_missing"},
        {"schema": ""},
        {"schema": schema, "name": "missing_table"},
    ]:
        assert _tables(conn, types=["TABLE"], **filters) == {}, filters
    # A same-name object in another schema must not leak into an exact schema
    # lookup. Verify the other schema independently with its own column shape.
    for schema_name, columns in [(schema, ["value"]), (other, ["other"])]:
        got = _tables(conn, schema=schema_name, name="kept_t", types=["TABLE"], depth="all")
        assert list(got) == [("redis", schema_name, "kept_t")]
        assert got[("redis", schema_name, "kept_t")][0] == "TABLE"
        assert got[("redis", schema_name, "kept_t")][1][0] == columns[0]
    got = _tables(
        conn, schema=schema, name="kept%", types=["VIEW", "MATERIALIZED VIEW"],
        column="value", depth="all",
    )
    assert got == {
        ("redis", schema, "kept_v"): ("VIEW", ["value"]),
        ("redis", schema, "kept_mv"): ("MATERIALIZED VIEW", ["value"]),
    }
    got = _tables(
        conn, schema=schema, name="kept_t", types=["TABLE"],
        column="missing_column", depth="all",
    )
    assert got == {("redis", schema, "kept_t"): ("TABLE", [])}
