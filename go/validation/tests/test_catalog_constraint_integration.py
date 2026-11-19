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

"""Cross-feature native regressions for catalog isolation and declarations."""

import uuid

import adbc_driver_manager
import pyarrow as pa
import pytest


def _catalog(conn, name):
    conn.adbc_connection.set_options(
        **{adbc_driver_manager.ConnectionOptions.CURRENT_CATALOG.value: name}
    )


def _objects(conn, catalog, schema):
    with conn.adbc_get_objects(
        depth="all", catalog_filter=catalog, db_schema_filter=schema,
        table_types_filter=["TABLE"],
    ) as reader:
        rows = reader.read_all().to_pylist()
    # The internal catalog/schema encoding must never reach public metadata.
    assert "\x00" not in repr(rows) and "\\x00" not in repr(rows)
    return {
        table["table_name"]: table
        for cat in rows
        if cat["catalog_name"] == catalog
        for sch in cat["catalog_db_schemas"]
        if sch["db_schema_name"] == schema
        for table in sch["db_schema_tables"]
    }


def _declaration(conn, catalog, schema, table, kind):
    matches = [
        constraint
        for constraint in _objects(conn, catalog, schema)[table]["table_constraints"]
        if constraint["constraint_type"] == kind
    ]
    assert len(matches) == 1
    return matches[0]


def _usage(catalog, schema, table="parent", column="id"):
    return [{
        "fk_catalog": catalog,
        "fk_db_schema": schema,
        "fk_table": table,
        "fk_column_name": column,
    }]


@pytest.fixture
def catalog_declarations(conn):
    suffix = uuid.uuid4().hex[:12]
    catalog = "it_declcat_" + suffix
    schema = "it_declscope_" + suffix
    with conn.cursor() as cur:
        cur.execute(f"CREATE DATABASE {catalog}")
        try:
            for name, value, extra in [
                ("redis", 101, "default_only"), (catalog, 202, "catalog_only"),
            ]:
                _catalog(conn, name)
                cur.execute(f"CREATE SCHEMA {schema}")
                cur.execute(
                    f"CREATE TABLE {schema}.parent (id INTEGER PRIMARY KEY, {extra} TEXT)"
                )
                cur.execute(
                    f"CREATE TABLE {schema}.child (parent_id INTEGER, label TEXT, "
                    "CONSTRAINT child_unique UNIQUE (label, parent_id), "
                    f"CONSTRAINT child_parent FOREIGN KEY (parent_id) "
                    f"REFERENCES {name}.{schema}.parent (id))"
                )
                cur.execute(f"INSERT INTO {schema}.parent VALUES ({value}, '{extra}')")
                cur.execute(f"INSERT INTO {schema}.child VALUES ({value}, '{extra}')")
            _catalog(conn, "redis")
            yield catalog, schema
        finally:
            _catalog(conn, "redis")
            cur.execute(f"DROP SCHEMA IF EXISTS {schema} CASCADE")
            cur.execute(f"DROP DATABASE IF EXISTS {catalog} CASCADE")


def test_catalog_constraint_metadata_isolation(conn, catalog_declarations):
    catalog, schema = catalog_declarations
    for name, value, extra in [
        ("redis", 101, "default_only"), (catalog, 202, "catalog_only"),
    ]:
        tables = _objects(conn, name, schema)
        assert set(tables) == {"parent", "child"}
        assert [c["column_name"] for c in tables["parent"]["table_columns"]] == ["id", extra]
        unique = _declaration(conn, name, schema, "child", "UNIQUE")
        assert unique["constraint_name"] == "child_unique"
        assert unique["constraint_column_names"] == ["label", "parent_id"]
        assert unique["constraint_column_usage"] is None
        foreign = _declaration(conn, name, schema, "child", "FOREIGN KEY")
        assert foreign["constraint_name"] == "child_parent"
        assert foreign["constraint_column_names"] == ["parent_id"]
        assert foreign["constraint_column_usage"] == _usage(name, schema)
        with conn.cursor() as cur:
            cur.execute(f"SELECT * FROM {name}.{schema}.parent")
            assert cur.fetchall() == [(value, extra)]


def test_catalog_constraint_rename_and_name_reuse(conn, catalog_declarations):
    catalog, schema = catalog_declarations
    with conn.cursor() as cur:
        cur.execute(f"ALTER TABLE {catalog}.{schema}.parent RENAME TO renamed_parent")
        cur.execute(f"ALTER TABLE {catalog}.{schema}.renamed_parent RENAME COLUMN id TO renamed_id")
        cur.execute(f"ALTER TABLE {catalog}.{schema}.child RENAME TO renamed_child")
        cur.execute(f"ALTER TABLE {catalog}.{schema}.renamed_child RENAME COLUMN parent_id TO renamed_ref")
        # A new table at the old name must not steal a stable reference.
        cur.execute(f"CREATE TABLE {catalog}.{schema}.parent (id INTEGER PRIMARY KEY)")
        foreign = _declaration(conn, catalog, schema, "renamed_child", "FOREIGN KEY")
        assert foreign["constraint_name"] == "child_parent"
        assert foreign["constraint_column_names"] == ["renamed_ref"]
        assert foreign["constraint_column_usage"] == _usage(
            catalog, schema, "renamed_parent", "renamed_id"
        )
        unique = _declaration(conn, catalog, schema, "renamed_child", "UNIQUE")
        assert unique["constraint_column_names"] == ["label", "renamed_ref"]
        assert unique["constraint_column_usage"] is None
        cur.execute(f"DROP TABLE {catalog}.{schema}.renamed_parent")
        cur.execute(
            f"CREATE TABLE {catalog}.{schema}.renamed_parent (renamed_id INTEGER PRIMARY KEY)"
        )
    foreign = _declaration(conn, catalog, schema, "renamed_child", "FOREIGN KEY")
    assert foreign["constraint_column_usage"] is None
    assert _declaration(conn, "redis", schema, "child", "FOREIGN KEY")[
        "constraint_column_usage"
    ] == _usage("redis", schema)
    with conn.cursor() as cur:
        cur.execute(f"SELECT * FROM redis.{schema}.parent")
        assert cur.fetchall() == [(101, "default_only")]


def test_catalog_arrow_replace_and_unknown_catalog_input(conn, catalog_declarations):
    catalog, schema = catalog_declarations
    replacement = pa.table({"replacement": pa.array([303], pa.int32())})
    with conn.cursor() as cur:
        assert cur.adbc_ingest(
            "parent", replacement, mode="replace",
            catalog_name=catalog, db_schema_name=schema,
        ) == 1
        cur.execute(f"SELECT * FROM {catalog}.{schema}.parent")
        assert cur.fetchall() == [(303,)]
        cur.execute(f"SELECT * FROM redis.{schema}.parent")
        assert cur.fetchall() == [(101, "default_only")]
    assert _declaration(conn, "redis", schema, "child", "FOREIGN KEY")[
        "constraint_column_usage"
    ] == _usage("redis", schema)

    consumed = []

    def batches():
        consumed.append(True)
        yield replacement.to_batches()[0]

    with pa.RecordBatchReader.from_batches(replacement.schema, batches()) as source:
        with conn.cursor() as cur:
            with pytest.raises(adbc_driver_manager.Error, match="catalog"):
                cur.adbc_ingest(
                    "parent", source, mode="replace",
                    catalog_name=catalog + "_missing", db_schema_name=schema,
                )
    assert consumed == []
    with conn.cursor() as cur:
        cur.execute(f"SELECT * FROM {catalog}.{schema}.parent")
        assert cur.fetchall() == [(303,)]
        cur.execute(f"SELECT * FROM redis.{schema}.parent")
        assert cur.fetchall() == [(101, "default_only")]
