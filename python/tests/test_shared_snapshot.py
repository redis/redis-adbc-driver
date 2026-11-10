"""Native connection options preserve one full-refresh read view (#365)."""

import json
from uuid import uuid4

import adbc_driver_manager.dbapi as manager
import pytest
from adbc_driver_redis import dbapi


def test_shared_read_snapshot_full_refresh(uri):
    orders, items = ("it_shared_" + uuid4().hex for _ in range(2))
    with dbapi.connect(uri) as reader, dbapi.connect(uri) as writer:
        with writer.cursor() as cur:
            cur.execute(f'CREATE TABLE "{orders}"(id INTEGER, value BIGINT)')
            cur.execute(f'CREATE TABLE "{items}"(id INTEGER, label VARCHAR)')
            cur.execute(f'INSERT INTO "{orders}" VALUES(1,9007199254740993),(1,NULL)')
            cur.execute(f"INSERT INTO \"{items}\" VALUES(1,'old'),(1,'old')")
        try:
            native = reader.adbc_connection
            native.set_options(**{"adbc.redis.read_snapshot_retention_ms": "120000"})
            native.set_options(**{"adbc.redis.read_snapshot_action": "acquire"})
            try:
                generation = json.loads(
                    native.get_option("adbc.redis.read_snapshot_status")
                )["generation"]
                schemas = {
                    name: reader.adbc_get_table_schema(name, db_schema_filter="public")
                    for name in (orders, items)
                }
                before = reader.adbc_get_objects(
                    db_schema_filter="public", table_name_filter=items
                ).read_all()
                with reader.cursor() as cur:
                    cur.execute(f'SELECT * FROM "{orders}" ORDER BY value NULLS LAST')
                    result = cur.fetch_arrow_table()
                assert result.schema == schemas[orders]
                assert result.to_pylist() == [
                    {"id": 1, "value": 9007199254740993},
                    {"id": 1, "value": None},
                ]
                with writer.cursor() as cur:
                    cur.execute(f'ALTER TABLE "{items}" ADD COLUMN extra INTEGER')
                    cur.execute(f'TRUNCATE "{items}"')
                    cur.execute(f'DROP TABLE "{orders}"')
                assert schemas[items] == reader.adbc_get_table_schema(
                    items, db_schema_filter="public"
                )
                assert before.equals(
                    reader.adbc_get_objects(
                        db_schema_filter="public", table_name_filter=items
                    ).read_all()
                )
                with reader.cursor() as cur:
                    cur.execute(f'SELECT * FROM "{items}"')
                    result = cur.fetch_arrow_table()
                assert result.schema == schemas[items]
                assert result.to_pylist() == [{"id": 1, "label": "old"}] * 2
                assert json.loads(
                    native.get_option("adbc.redis.read_snapshot_status")
                )["generation"] == generation
                with pytest.raises(manager.Error, match="read-only"):
                    with reader.cursor() as cur:
                        cur.execute(f'DELETE FROM "{items}"')
            finally:
                native.set_options(**{"adbc.redis.read_snapshot_action": "release"})
            with reader.cursor() as cur:
                cur.execute(f'SELECT COUNT(*) FROM "{items}"')
                assert cur.fetchall() == [(0,)]
        finally:
            with writer.cursor() as cur:
                cur.execute(f'DROP TABLE IF EXISTS "{orders}"')
                cur.execute(f'DROP TABLE IF EXISTS "{items}"')
