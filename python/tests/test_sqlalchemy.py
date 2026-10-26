import datetime

import pytest
from sqlalchemy import create_engine, inspect, text
from sqlalchemy.exc import ArgumentError, InvalidRequestError


def test_engine_reflection_and_charts(uri):
    engine = create_engine(uri.replace("://", "+adbc://", 1), future=True)
    try:
        with engine.connect() as conn:
            conn.exec_driver_sql("DROP SCHEMA IF EXISTS it_python_sa CASCADE")
            conn.exec_driver_sql("CREATE SCHEMA it_python_sa")
            try:
                conn.exec_driver_sql(
                    "CREATE TABLE it_python_sa.sales (id integer PRIMARY KEY, ts timestamp, country varchar(20), amount numeric(12,2))"
                )
                conn.exec_driver_sql(
                    "INSERT INTO it_python_sa.sales VALUES (1, TIMESTAMP '2026-01-01 01:00:00', 'US', 10.25), (2, TIMESTAMP '2026-01-01 02:00:00', 'US', 20.50), (3, TIMESTAMP '2026-01-02', 'CA', 5.00)"
                )
                conn.exec_driver_sql(
                    "CREATE VIEW it_python_sa.v AS SELECT id FROM it_python_sa.sales"
                )
                inspector = inspect(conn)
                assert "it_python_sa" in inspector.get_schema_names()
                assert inspector.get_table_names(schema="it_python_sa") == ["sales"]
                assert inspector.get_view_names(schema="it_python_sa") == ["v"]
                assert inspector.has_table("sales", schema="it_python_sa")
                cols = inspector.get_columns("sales", schema="it_python_sa")
                assert [c["name"] for c in cols] == ["id", "ts", "country", "amount"]
                assert cols[2]["type"].length == 20
                assert cols[3]["type"].precision == 12 and cols[3]["type"].scale == 2
                assert inspector.get_pk_constraint("sales", schema="it_python_sa")[
                    "constrained_columns"
                ] == ["id"]
                assert inspector.get_foreign_keys("sales", schema="it_python_sa") == []
                assert inspector.get_indexes("sales", schema="it_python_sa")
                assert inspector.get_table_comment("sales", schema="it_python_sa") == {
                    "text": None
                }
                assert (
                    conn.execute(
                        text("SELECT country FROM it_python_sa.sales WHERE id=:id"),
                        {"id": 1},
                    ).scalar()
                    == "US"
                )
                rows = conn.exec_driver_sql(
                    "SELECT DATE_TRUNC('day',ts) AS __timestamp, SUM(amount) AS \"SUM(amount)\" FROM it_python_sa.sales GROUP BY 1 ORDER BY 1"
                ).all()
                assert [str(r[1]) for r in rows] == ["30.75", "5.00"]
                assert rows[0][0] == datetime.datetime(2026, 1, 1)
                assert (
                    conn.exec_driver_sql(
                        "SELECT country, SUM(amount) AS total FROM it_python_sa.sales GROUP BY country ORDER BY total DESC"
                    ).all()[0][0]
                    == "US"
                )
                with pytest.raises(InvalidRequestError, match="savepoints"):
                    conn.begin_nested()
            finally:
                conn.exec_driver_sql("DROP SCHEMA it_python_sa CASCADE")
        with engine.connect() as conn, pytest.raises(ArgumentError):
            conn.execution_options(isolation_level="SERIALIZABLE")
    finally:
        engine.dispose()


def test_url_escaping():
    from adbc_driver_redis.sqlalchemy import RedisDialect
    from sqlalchemy.engine import make_url

    _, kwargs = RedisDialect().create_connect_args(
        make_url("rediss+adbc://u:p%40ss@[::1]:6380/0?read_timeout=30s")
    )
    assert kwargs["uri"] == "rediss://u:p%40ss@[::1]:6380/0?read_timeout=30s"
