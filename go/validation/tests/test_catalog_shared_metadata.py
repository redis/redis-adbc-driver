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

"""Named-catalog metadata follows the default reader's shared snapshot."""

import uuid

import adbc_driver_manager
import adbc_driver_manager.dbapi
import pytest

from shared_snapshot_validation import get_objects_records


ACTION = "adbc.redis.read_snapshot_action"


def _metadata(reader, catalog, table, expected_columns):
    with reader.adbc_get_objects(
        depth="all", catalog_filter=catalog, db_schema_filter="public",
        table_name_filter=table,
    ) as stream:
        objects = stream.read_all()
    records = get_objects_records(objects)
    assert len(records) == 1
    assert records[0]["catalog_name"] == catalog
    schemas = records[0]["catalog_db_schemas"]
    assert len(schemas) == 1
    assert schemas[0]["db_schema_name"] == "public"
    tables = schemas[0]["db_schema_tables"]
    assert len(tables) == 1
    assert tables[0]["table_name"] == table
    assert [column["column_name"] for column in tables[0]["table_columns"]] == expected_columns
    schema = reader.adbc_get_table_schema(
        table, catalog_filter=catalog, db_schema_filter="public",
    )
    assert schema.names == expected_columns
    # Internal catalog identities must not appear in either metadata surface.
    assert "\\x00" not in repr(records)
    assert "\\x00" not in repr(schema)
    return records, schema


def _named_rows_unsupported(reader, catalog, table):
    with reader.cursor() as cursor:
        with pytest.raises(adbc_driver_manager.NotSupportedError):
            cursor.execute(f"SELECT * FROM {catalog}.public.{table}")


def test_shared_snapshot_pins_named_catalog_metadata(driver_path, db_kwargs, conn):
    catalog = "it_sharedcat_" + uuid.uuid4().hex[:12]
    table = "pinned_shape"
    options = dict(db_kwargs)
    options["adbc.redis.telemetry.enabled"] = "false"
    with conn.cursor() as writer:
        writer.execute(f"CREATE DATABASE {catalog}")
        try:
            writer.execute(
                f"CREATE TABLE {catalog}.public.{table} (id INTEGER, original TEXT)"
            )
            with adbc_driver_manager.dbapi.connect(
                driver=driver_path, db_kwargs=options, autocommit=True,
            ) as reader:
                handle = reader.adbc_connection
                assert handle.get_option("adbc.connection.catalog") == "redis"
                try:
                    handle.set_options(**{"adbc.redis.read_snapshot_retention_ms": 120000})
                    handle.set_options(**{ACTION: "acquire"})
                    before, old_schema = _metadata(
                        reader, catalog, table, ["id", "original"]
                    )
                    _named_rows_unsupported(reader, catalog, table)
                    writer.execute(
                        f"ALTER TABLE {catalog}.public.{table} ADD COLUMN added INTEGER"
                    )
                    after, schema = _metadata(reader, catalog, table, ["id", "original"])
                    assert after == before
                    assert schema.equals(old_schema, check_metadata=True)
                    writer.execute(f"DROP TABLE {catalog}.public.{table}")
                    writer.execute(
                        f"CREATE TABLE {catalog}.public.{table} (replacement BIGINT)"
                    )
                    after, schema = _metadata(reader, catalog, table, ["id", "original"])
                    assert after == before
                    assert schema.equals(old_schema, check_metadata=True)
                    _named_rows_unsupported(reader, catalog, table)

                    # Refresh is an explicit new full read attempt: release then
                    # acquire. There is no in-place refresh action.
                    handle.set_options(**{ACTION: "release"})
                    handle.set_options(**{ACTION: "acquire"})
                    fresh, fresh_schema = _metadata(
                        reader, catalog, table, ["replacement"]
                    )
                    assert fresh != before
                    assert not fresh_schema.equals(old_schema)
                    _named_rows_unsupported(reader, catalog, table)
                    assert handle.get_option("adbc.connection.catalog") == "redis"
                finally:
                    # The bound view must be released before deleting the catalog.
                    handle.set_options(**{ACTION: "release"})
        finally:
            writer.execute(f"DROP DATABASE IF EXISTS {catalog} CASCADE")
