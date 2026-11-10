"""Native C/Python shared-view regression; use a dedicated Software database.

Set REDIS_URI and SSL_CERT_FILE after make redis-up. Pass --driver PATH to the
freshly built native library. This creates and drops two issue365_* test tables.
"""

import argparse
import json
import os

import adbc_driver_manager.dbapi


def get_objects_records(table):
    """Canonicalize unordered discovery collections, preserving all metadata.

    Driverbase collects schema callbacks concurrently. GetObjects does not
    promise catalog/schema/table order; column ordinal/order and every field
    value still participate in the exact comparison, as do duplicate entries.
    """
    records = table.to_pylist()
    key = lambda record: json.dumps(record, sort_keys=True)
    for catalog in records:
        schemas = catalog.get("catalog_db_schemas")
        if schemas is None:
            continue
        for schema in schemas:
            tables = schema.get("db_schema_tables")
            if tables is not None:
                tables.sort(key=key)
        schemas.sort(key=key)
    records.sort(key=key)
    return records


def validate(driver, db_kwargs=None):
    # The standard validation suite supplies the exact native/wheel endpoint
    # options (including explicit TLS trust) through its existing fixture.
    database_options = (
        dict(db_kwargs) if db_kwargs is not None else {"uri": os.environ["REDIS_URI"]}
    )
    database_options["adbc.redis.telemetry.enabled"] = "false"

    def connect():
        return adbc_driver_manager.dbapi.connect(
            driver=driver,
            db_kwargs=database_options,
            autocommit=True,
        )

    with connect() as reader, connect() as writer:
        with writer.cursor() as cur:
            cur.execute("CREATE TABLE issue365_orders(id INTEGER, v BIGINT)")
            cur.execute("CREATE TABLE issue365_items(id INTEGER, v BIGINT)")
            cur.execute(
                "INSERT INTO issue365_orders VALUES "
                "(1,9007199254740993),(1,9007199254740993),(2,NULL)"
            )
            cur.execute("INSERT INTO issue365_items VALUES (1,10),(1,10),(2,20)")
        options = reader.adbc_connection
        try:
            options.set_options(**{"adbc.redis.read_snapshot_retention_ms": 120000})
            options.set_options(**{"adbc.redis.read_snapshot_action": "acquire"})
            status = json.loads(options.get_option("adbc.redis.read_snapshot_status"))
            with reader.adbc_get_objects() as meta:
                before_objects = meta.read_all()
            schema = reader.adbc_get_table_schema("issue365_items")
            with reader.cursor() as cur:
                cur.execute("SELECT id,v FROM issue365_orders ORDER BY id")
                assert cur.fetchall() == [
                    (1, 9007199254740993), (1, 9007199254740993), (2, None)
                ]
            with writer.cursor() as cur:
                cur.execute("UPDATE issue365_items SET v=99")
                cur.execute("ALTER TABLE issue365_items ADD COLUMN newer INTEGER")
                cur.execute("DELETE FROM issue365_items WHERE id=2")
                cur.execute("SELECT id,v FROM issue365_items ORDER BY id")
                assert cur.fetchall() == [(1, 99), (1, 99)]
            assert reader.adbc_get_table_schema("issue365_items").equals(schema)
            with reader.adbc_get_objects() as meta:
                after_objects = meta.read_all()
                assert after_objects.schema.equals(before_objects.schema, check_metadata=True)
                assert get_objects_records(after_objects) == get_objects_records(before_objects)
            with reader.cursor() as cur:
                cur.execute("SELECT id,v FROM issue365_items ORDER BY id")
                assert cur.fetchall() == [(1, 10), (1, 10), (2, 20)]
            assert json.loads(options.get_option("adbc.redis.read_snapshot_status")) == status
        finally:
            options.set_options(**{"adbc.redis.read_snapshot_action": "release"})
            with writer.cursor() as cur:
                cur.execute("DROP TABLE issue365_items")
                cur.execute("DROP TABLE issue365_orders")
        assert json.loads(options.get_option("adbc.redis.read_snapshot_status")) == {
            "state": "unbound"
        }
    print("PASS: native Python metadata, exact values, multiplicities and generation identity")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--driver", required=True)
    validate(parser.parse_args().driver)
