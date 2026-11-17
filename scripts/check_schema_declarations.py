#!/usr/bin/env python3
# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Check #453 through the native ADBC C API and optional standard Flight SQL.

Requires pyarrow, adbc-driver-manager and (for --flight-uri) adbc-driver-flightsql.
Use only a disposable, verified Redis Software 4x QPF fixture. The caller must
verify deployment policy before setting REDIS_RETAINED_CONTROL_PERSISTENCE.
No endpoints, credentials or operational artifacts are emitted.
"""
import argparse
import os
import uuid

import adbc_driver_manager.dbapi as native
import pyarrow as pa

KEY = b"ARROW:FLIGHT:SQL:TYPE_NAME"
REMARKS = b"ARROW:FLIGHT:SQL:REMARKS"
DECLARATIONS = [b"NUMERIC(12,2)", b"VARCHAR(17)", b"CHAR(4)", b"TIMESTAMP(2)",
                b"TIME(4)", b"TIMESTAMP(5) WITH TIME ZONE", b"JSON", b"UUID",
                b"TIME(2) WITH TIME ZONE"]
DDL = """(amount NUMERIC(12,2), label VARCHAR(17), padded CHAR(4),
 occurred TIMESTAMP(2), clock TIME(4), zoned TIMESTAMP(5) WITH TIME ZONE,
 doc JSON, ident UUID, offset_clock TIME(2) WITH TIME ZONE)"""


def check(schema):
    assert [f.metadata.get(KEY) if f.metadata else None for f in schema] == DECLARATIONS, schema
    assert schema.field("amount").type == pa.decimal128(12, 2)
    assert schema.field("label").type == pa.string()
    assert schema.field("padded").type == pa.string()
    assert schema.field("occurred").type == pa.timestamp("ms")
    assert schema.field("clock").type == pa.time64("us")
    for field in schema:
        if field.name != "doc":
            assert b"ARROW:extension:name" not in (field.metadata or {}), field


def probe(conn, name, *, check_ingest=False):
    schema_name = "TypeMetadata453_" + uuid.uuid4().hex[:12]
    with conn.cursor() as cur:
        cur.execute(f'CREATE SCHEMA "{schema_name}"')
        try:
            table = f'"{schema_name}"."EmptyTable"'
            cur.execute(f"CREATE TABLE {table} {DDL}")
            cur.execute(f"COMMENT ON COLUMN {table}.label IS 'label comment'")
            cur.execute(f'CREATE VIEW "{schema_name}"."EmptyView" AS SELECT * FROM {table}')
            for relation in ("EmptyTable", "EmptyView"):
                schema = conn.adbc_get_table_schema(relation, catalog_filter="redis", db_schema_filter=schema_name)
                check(schema)
                if relation == "EmptyTable":
                    assert schema.field("label").metadata[REMARKS] == b"label comment"
                cur.execute(f'SELECT * FROM "{schema_name}"."{relation}"')
                result = cur.fetch_arrow_table()
                assert result.num_rows == 0
                check(result.schema)
            objects = conn.adbc_get_objects(depth="all", catalog_filter="redis",
                                           db_schema_filter=schema_name).read_all().to_pylist()
            tables = [table for cat in objects for db in cat["catalog_db_schemas"]
                      for table in db["db_schema_tables"]]
            assert {t["table_name"] for t in tables} == {"EmptyTable", "EmptyView"}, tables
            for table_info in tables:
                assert [c["xdbc_type_name"].encode() for c in table_info["table_columns"]] == DECLARATIONS
            if check_ingest:
                # TYPE_NAME alone is descriptive: ordinary strings do not become
                # extensions or inherit declaration constraints during ingest.
                field = conn.adbc_get_table_schema("EmptyTable", db_schema_filter=schema_name).field("label")
                value = "longer than seventeen characters"
                data = pa.Table.from_arrays([pa.array([value])], schema=pa.schema([field]))
                assert cur.adbc_ingest("ArrowInput", data, db_schema_name=schema_name) == 1
                cur.execute(f'SELECT label FROM "{schema_name}"."ArrowInput"')
                assert cur.fetchone() == (value,)
            # Data conversion still works after schema discovery.
            cur.execute(f"INSERT INTO {table} (amount,label,occurred) VALUES (12.34,'hello',TIMESTAMP '2026-01-01 00:00:00.12')")
            cur.execute(f"SELECT label FROM {table}")
            assert cur.fetchone() == ("hello",)
        finally:
            cur.execute(f'DROP SCHEMA "{schema_name}" CASCADE')
    print(f"{name}: declarations, physical types, empty table/view, case, comments, GetObjects and values passed")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--library", required=True)
    parser.add_argument("--flight-uri", help="Optional running Flight service compiled with the candidate driver")
    args = parser.parse_args()
    options = {"uri": os.environ["REDIS_URI"]}
    if os.environ.get("SSL_CERT_FILE"):
        options["adbc.redis.tls_ca_file"] = os.environ["SSL_CERT_FILE"]
    with native.connect(driver=args.library, db_kwargs=options, autocommit=True) as conn:
        probe(conn, "native ADBC", check_ingest=True)
    if args.flight_uri:
        import adbc_driver_flightsql.dbapi as flightsql
        with flightsql.connect(args.flight_uri, autocommit=True) as conn:
            probe(conn, "standard ADBC Flight SQL")


if __name__ == "__main__":
    main()
