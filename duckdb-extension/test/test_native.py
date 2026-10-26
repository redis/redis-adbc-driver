"""Integration checks for the compiled extension against a disposable Redis.

REDIS_ADBC_EXTENSION=/absolute/path/redis_adbc.duckdb_extension \
REDIS_ADBC_LIBRARY=/absolute/path/libadbc_driver_redis.so \
REDIS_URI=redis://localhost:16389/0 python -m pytest duckdb-extension/test -q
"""

import datetime
import decimal
import os
import uuid

import adbc_driver_manager.dbapi
import duckdb
import pyarrow as pa
import pytest


@pytest.fixture
def db():
    extension = os.environ.get("REDIS_ADBC_EXTENSION")
    if not extension:
        pytest.skip("set REDIS_ADBC_EXTENSION to a compiled extension")
    with duckdb.connect(config={"allow_unsigned_extensions": True}) as conn:
        conn.execute("LOAD '" + extension.replace("'", "''") + "'")
        yield conn


@pytest.fixture
def uri():
    value = os.environ.get("REDIS_URI")
    if not value:
        pytest.skip("set REDIS_URI to a disposable Redis with Search")
    return value


@pytest.fixture
def data(uri):
    library = os.environ["REDIS_ADBC_LIBRARY"]
    name = "duckdb_native_" + uuid.uuid4().hex
    with adbc_driver_manager.dbapi.connect(driver=library, db_kwargs={"uri": uri}, autocommit=True) as conn:
        with conn.cursor() as cur:
            batch = pa.table({
                "id": pa.array(range(10003), type=pa.int64()),
                "label": [None if i % 17 == 0 else f"row-{i}-é" for i in range(10003)],
                "flag": [i % 2 == 0 for i in range(10003)],
                "amount": pa.array([decimal.Decimal("123.45")] * 10003, type=pa.decimal128(12, 2)),
                "day": pa.array([datetime.date(2026, 10, 20)] * 10003, type=pa.date32()),
            })
            cur.adbc_ingest(name, batch, mode="create")
            try:
                yield name, batch
            finally:
                cur.execute(f'DROP TABLE public."{name}"')


def scan(db, uri, sql, prefix="SELECT * FROM", suffix=""):
    return db.execute(f"{prefix} redis_adbc_scan(?, ?) {suffix}", [uri, sql])


def test_streamed_values_and_types(db, uri, data):
    name, expected = data
    actual = scan(db, uri, f"SELECT * FROM public.{name} ORDER BY id").to_arrow_table()
    assert actual.column_names == expected.column_names
    assert actual.to_pylist() == expected.to_pylist()
    assert actual.schema == expected.schema


def test_projection_filter_count_join_and_early_close(db, uri, data):
    name, _ = data
    query = f"SELECT * FROM public.{name}"
    assert scan(db, uri, query, "SELECT count(*) FROM").fetchall() == [(10003,)]
    assert scan(db, uri, query, "SELECT label, id FROM", "WHERE id=42").fetchall() == [("row-42-é", 42)]
    assert scan(db, uri, query, suffix="LIMIT 0").fetchall() == []
    assert len(scan(db, uri, query, suffix="LIMIT 1").fetchall()) == 1
    assert scan(db, uri, query, "SELECT s.id, x.label FROM", "s JOIN (VALUES (42,'answer')) x(id,label) USING (id)").fetchall() == [(42, "answer")]
    assert scan(db, uri, f"SELECT * FROM public.{name} WHERE id < 0").fetchall() == []
    # Finish another complete fetch after early stream termination.
    assert scan(db, uri, query, "SELECT sum(id) FROM").fetchall() == [(10003 * 10002 // 2,)]


@pytest.mark.parametrize("sql", [
    "CREATE TABLE should_not_exist (id integer)",
    "INSERT INTO should_not_exist VALUES (1)",
    "SELECT 1; DROP TABLE should_not_exist",
    "SELECT 1; SELECT 2",
    "", "-- empty",
])
def test_rejects_non_read_queries_before_connecting(db, sql):
    with pytest.raises(duckdb.Error, match="exactly one SELECT|empty query"):
        scan(db, "redis://127.0.0.1:1/0", sql).fetchall()


@pytest.mark.parametrize("uri,sql", [(None, "SELECT 1"), ("redis://localhost", None), ("https://localhost", "SELECT 1")])
def test_invalid_arguments(db, uri, sql):
    with pytest.raises(duckdb.Error, match="must not be NULL|requires a redis"):
        scan(db, uri, sql).fetchall()


def test_select_cte_and_prepared_reexecution(db, uri):
    assert scan(db, uri, "WITH x AS (SELECT 7 AS n) SELECT n FROM x UNION ALL SELECT 8").fetchall() == [(7,), (8,)]
    quoted_uri = "'" + uri.replace("'", "''") + "'"
    db.execute(f"PREPARE q AS SELECT * FROM redis_adbc_scan({quoted_uri}, 'SELECT 42 AS n')")
    assert db.execute("EXECUTE q").fetchall() == [(42,)]
    assert db.execute("EXECUTE q").fetchall() == [(42,)]


def test_external_access_disabled(db):
    db.execute("SET enable_external_access=false")
    with pytest.raises(duckdb.Error, match="enable_external_access"):
        scan(db, "redis://127.0.0.1:1/0", "SELECT 1").fetchall()


def test_connection_failure_releases_partial_handles(db):
    for _ in range(3):
        with pytest.raises(duckdb.Error):
            scan(db, "redis://127.0.0.1:1/0?dial_timeout=100ms", "SELECT 1").fetchall()


def test_stream_error_then_successful_query(db, uri, data):
    name, _ = data
    with pytest.raises(duckdb.Error, match="division by zero"):
        scan(db, uri, f"SELECT CASE WHEN id=3000 THEN 1/0 ELSE id END AS n FROM public.{name}").fetchall()
    assert scan(db, uri, "SELECT 42 AS n").fetchall() == [(42,)]


def test_prepared_statement_detects_changed_schema(db, uri):
    name = "duckdb_schema_" + uuid.uuid4().hex
    with adbc_driver_manager.dbapi.connect(driver=os.environ["REDIS_ADBC_LIBRARY"], db_kwargs={"uri": uri}, autocommit=True) as conn:
        with conn.cursor() as cur:
            cur.execute(f"CREATE TABLE public.{name} (id integer)")
            try:
                quoted_uri = "'" + uri.replace("'", "''") + "'"
                db.execute(f"PREPARE schema_q AS SELECT * FROM redis_adbc_scan({quoted_uri}, 'SELECT * FROM public.{name}')")
                cur.execute(f"DROP TABLE public.{name}")
                cur.execute(f"CREATE TABLE public.{name} (id text)")
                with pytest.raises(duckdb.Error, match="schema changed"):
                    db.execute("EXECUTE schema_q").fetchall()
            finally:
                cur.execute(f"DROP TABLE public.{name}")
