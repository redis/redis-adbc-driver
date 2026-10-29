"""Airflow checks run in a separate environment with the airflow extra."""

import json
import uuid
from unittest.mock import Mock

import pytest

pytest.importorskip("airflow.providers.apache.arrow.hooks.adbc")

from airflow.models.connection import Connection
from adbc_driver_redis.airflow import RedisAdbcHook


def make_hook(uri):
    connection = Connection(
        conn_id="redis_test",
        conn_type="adbc",
        extra=json.dumps({"dialect": "redis", "db_kwargs": {"uri": uri}}),
    )
    return RedisAdbcHook(adbc_conn_id="redis_test", connection=connection)


def test_get_records(uri):
    hook = make_hook(uri)
    assert hook.get_records("SELECT 1 AS one") == [(1,)]
    assert hook.get_records("SELECT $1 AS value", parameters=(42,)) == [(42,)]
    assert hook.get_records("SELECT 1 WHERE FALSE") == []
    assert hook.get_records(["SELECT 1", "SELECT 2"]) == [[(1,)], [(2,)]]
    assert hook.get_first("SELECT 3") == (3,)


def test_get_records_after_writes(uri):
    hook = make_hook(uri)
    table = "it_airflow_" + uuid.uuid4().hex
    hook.run(f"CREATE TABLE {table} (id integer, name text)")
    try:
        hook.insert_rows(
            table,
            [(1, "Alice"), (2, None)],
            target_fields=["id", "name"],
            autocommit=True,
        )
        assert hook.get_records(f"SELECT id, name FROM {table} ORDER BY id") == [
            (1, "Alice"),
            (2, None),
        ]
    finally:
        hook.run(f"DROP TABLE {table}")


def test_reject_transaction_before_connect(monkeypatch):
    import adbc_driver_manager.dbapi as manager

    hook = make_hook("redis://localhost:1/0")
    connect = Mock(side_effect=AssertionError("must reject before connecting"))
    monkeypatch.setattr(hook, "get_conn", connect)
    with pytest.raises(manager.NotSupportedError, match="autocommit=True"):
        hook.run("SELECT 1", autocommit=False)
    connect.assert_not_called()


def test_reject_transaction_setting():
    import adbc_driver_manager.dbapi as manager

    conn = Mock()
    with pytest.raises(manager.NotSupportedError, match="autocommit=True"):
        make_hook("redis://localhost:1/0").set_autocommit(conn, False)
    conn.adbc_connection.set_autocommit.assert_not_called()


def test_connection_options(monkeypatch):
    from adbc_driver_redis import dbapi

    hook = make_hook("rediss://localhost:1/0")
    hook.db_kwargs["adbc.redis.time_zone"] = "UTC"
    hook.conn_kwargs["adbc.connection.autocommit"] = "true"
    connect = Mock()
    monkeypatch.setattr(dbapi, "connect", connect)
    assert hook.get_conn() is connect.return_value
    connect.assert_called_once_with(
        "rediss://localhost:1/0",
        db_kwargs={"adbc.redis.time_zone": "UTC"},
        conn_kwargs={"adbc.connection.autocommit": "true"},
        entrypoint=None,
    )
    assert hook.db_kwargs["uri"] == "rediss://localhost:1/0"
