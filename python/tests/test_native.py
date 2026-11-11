"""The Python binding exercises the actual native ABI; SQL is used only to write."""

from uuid import uuid4

import pyarrow as pa
import pytest

from adbc_driver_redis import dbapi
from adbc_driver_redis.native import NativeClient, NativeReadError


def test_native_hash_reads_and_snapshots(uri):
    name = "it_native_" + uuid4().hex
    items = name + "_items"
    options = {"adbc.redis.telemetry.enabled": "false"}
    with dbapi.connect(uri, db_kwargs=options) as writer:
        with writer.cursor() as cur:
            cur.execute(f'CREATE TABLE "{items}" (label TEXT)')
            cur.execute(f"INSERT INTO \"{items}\" VALUES ('old item')")
            cur.adbc_ingest(name, pa.table({
                "id": pa.array([101, None, 102], type=pa.int64()),
                "name": ["Alice", None, ""],
                "payload": pa.array([b"\x00\xffhello", None, b""], type=pa.binary()),
                "amount": pa.array([9007199254740993, None, -9007199254740993], type=pa.int64()),
            }))
        try:
            with NativeClient(uri, db_kwargs=options) as client:
                table = client.table(name)
                assert table.hgetall(1) == {
                    b"id": b"101", b"name": b"Alice", b"payload": b"\x00\xffhello",
                    b"amount": b"9007199254740993",
                }
                assert table.hget(3, "payload") == b""
                assert table.hexists(3, "payload")
                assert not table.hexists(2, "payload")
                assert table.exists(2) and table.hlen(2) == 0
                assert not table.exists(101)  # Application IDs are not __rowid.
                assert table.hgetall(101) == {}
                assert table.hmget(1, ["name", "missing", "__rowid", "name"]) == [b"Alice", None, None, b"Alice"]
                with client.snapshot() as view:
                    old = view.table(name)
                    with writer.cursor() as cur:
                        cur.execute(f'UPDATE "{name}" SET name=\'Bob\' WHERE id=101')
                        cur.execute(f'DELETE FROM "{name}" WHERE id=102')
                        cur.execute(f"UPDATE \"{items}\" SET label='new item'")
                    assert old.hget(1, "name") == b"Alice"
                    assert old.exists(3)
                    assert view.table(items).hget(1, "label") == b"old item"
                    assert client.table(items).hget(1, "label") == b"new item"
                    assert table.hget(1, "name") == b"Bob"
                    assert not table.exists(3)
                    with writer.cursor() as cur:
                        cur.execute(f'ALTER TABLE "{name}" RENAME COLUMN name TO label')
                        cur.execute(f'ALTER TABLE "{name}" ADD COLUMN extra TEXT DEFAULT \'added\'')
                    assert old.hget(1, "name") == b"Alice"
                    assert old.hget(1, "label") is None
                with pytest.raises(NativeReadError, match="closed"):
                    old.exists(1)
                assert table.hget(1, "label") == b"Bob"
                assert table.hget(1, "extra") == b"added"
                with writer.cursor() as cur:
                    cur.execute(f'UPDATE "{name}" SET extra=NULL WHERE id=101')
                assert table.hget(1, "extra") is None
                with pytest.raises(ValueError):
                    table.hmget(1, [])
                with pytest.raises(ValueError):
                    table.hgetall(True)
                with pytest.raises(ValueError):
                    table.hgetall(2**63)
                with pytest.raises(TypeError):
                    table.hmget(1, "name")
                with pytest.raises(ValueError):
                    with client.snapshot(retention_ms=1):
                        pass
                with pytest.raises(RuntimeError, match="consumer failure"):
                    with client.snapshot() as failed:
                        raise RuntimeError("consumer failure")
                with pytest.raises(NativeReadError, match="closed"):
                    failed.table(name).exists(1)
            client.close()  # Idempotent.
            with pytest.raises(NativeReadError, match="closed"):
                table.exists(1)
            with NativeClient(uri, db_kwargs=options, decode_responses=True) as text:
                assert text.table(name).hget(1, "label") == "Bob"
                with pytest.raises(UnicodeDecodeError):
                    text.table(name).hget(1, "payload")
        finally:
            with writer.cursor() as cur:
                cur.execute(f'DROP TABLE IF EXISTS "{name}"')
                cur.execute(f'DROP TABLE IF EXISTS "{items}"')


def test_native_identifiers_are_not_sql(uri):
    name = 'native; SELECT "雪" ' + uuid4().hex
    quoted = '"' + name.replace('"', '""') + '"'
    options = {"adbc.redis.telemetry.enabled": "false"}
    with dbapi.connect(uri, db_kwargs=options) as writer:
        with writer.cursor() as cur:
            cur.execute(f'CREATE TABLE {quoted} ("odd;field" TEXT)')
            cur.execute(f"INSERT INTO {quoted} VALUES ('exact')")
        try:
            with NativeClient(uri, db_kwargs=options, decode_responses=True) as client:
                assert client.table(name).hgetall(1) == {"odd;field": "exact"}
        finally:
            with writer.cursor() as cur:
                cur.execute(f'DROP TABLE {quoted}')
