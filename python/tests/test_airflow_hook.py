"""Airflow checks run in a separate environment with the airflow extra."""

import json
from importlib.metadata import entry_points
import uuid
from unittest.mock import Mock

import pytest

pytest.importorskip("airflow.providers.apache.arrow.hooks.adbc")

from airflow.models.connection import Connection
from adbc_driver_redis.airflow import RedisAdbcHook


def make_hook(uri, conn_type="redis_adbc"):
    connection = Connection(
        conn_id="redis_test",
        conn_type=conn_type,
        extra=json.dumps({"db_kwargs": {"uri": uri}}),
    )
    return RedisAdbcHook(adbc_conn_id="redis_test", connection=connection)


def test_provider_discovery():
    from airflow.providers_manager import ProvidersManager
    from airflow.providers.apache.arrow.hooks.adbc import AdbcHook

    entry, = [
        ep for ep in entry_points(group="apache_airflow_provider")
        if ep.dist.name == "adbc-driver-redis"
    ]
    assert entry.load()()["package-name"] == "adbc-driver-redis"
    providers = ProvidersManager()
    assert "adbc-driver-redis" in providers.providers
    assert providers.hooks["redis_adbc"].hook_class_name == (
        "adbc_driver_redis.airflow.RedisAdbcHook"
    )
    connection = Connection(conn_id="redis_analytics", conn_type="redis_adbc")
    hook = connection.get_hook()
    assert isinstance(hook, RedisAdbcHook)
    assert hook.get_conn_id() == "redis_analytics"
    from airflow.sdk import Connection as TaskConnection

    runtime_hook = TaskConnection(conn_id="redis_runtime", conn_type="redis_adbc").get_hook()
    assert isinstance(runtime_hook, RedisAdbcHook)
    assert runtime_hook.get_conn_id() == "redis_runtime"
    assert RedisAdbcHook().get_conn_id() == "redis_adbc_default"
    # The Redis provider must not take over generic ADBC connections.
    assert type(Connection(conn_type="adbc").get_hook()) is AdbcHook


def test_sql_operator(uri, monkeypatch):
    from airflow.providers.common.sql.operators.sql import SQLExecuteQueryOperator

    monkeypatch.setenv("AIRFLOW_CONN_REDIS_ANALYTICS", json.dumps({
        "conn_type": "redis_adbc",
        "extra": {"db_kwargs": {"uri": uri}},
    }))
    operator = SQLExecuteQueryOperator(
        task_id="redis_query", conn_id="redis_analytics",
        sql="SELECT $1 AS answer", parameters=(42,), autocommit=True,
    )
    hook = operator.get_db_hook()
    assert isinstance(hook, RedisAdbcHook)
    assert hook.get_conn_id() == "redis_analytics"
    assert operator.execute(context={}) == [(42,)]


def test_sql_operator_rejects_transactions(monkeypatch):
    from airflow.providers.common.sql.operators.sql import SQLExecuteQueryOperator
    from adbc_driver_redis import dbapi

    monkeypatch.setenv("AIRFLOW_CONN_REDIS_ANALYTICS", json.dumps({
        "conn_type": "redis_adbc",
        "extra": {"db_kwargs": {"uri": "redis://localhost:1/0"}},
    }))
    operator = SQLExecuteQueryOperator(
        task_id="redis_query", conn_id="redis_analytics", sql="SELECT 1",
    )
    connect = Mock(side_effect=AssertionError("must reject before connecting"))
    monkeypatch.setattr(dbapi, "connect", connect)
    with pytest.raises(dbapi.NotSupportedError, match="autocommit=True"):
        operator.execute(context={})
    connect.assert_not_called()


@pytest.mark.parametrize("conn_type", ["redis_adbc", "adbc"])
def test_get_records(uri, conn_type):
    hook = make_hook(uri, conn_type)
    assert hook.get_records("SELECT 1 AS one") == [(1,)]
    assert hook.get_records("SELECT $1 AS value", parameters=(42,)) == [(42,)]
    assert hook.get_records("SELECT 1 WHERE FALSE") == []
    assert hook.get_records(["SELECT 1", "SELECT 2"]) == [[(1,)], [(2,)]]
    assert hook.get_first("SELECT 3") == (3,)


@pytest.mark.parametrize("conn_type", ["redis_adbc", "adbc"])
def test_get_records_after_writes(uri, conn_type):
    hook = make_hook(uri, conn_type)
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
