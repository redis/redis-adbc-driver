import datetime
from decimal import Decimal
from uuid import UUID

import pytest
from sqlalchemy import create_engine, inspect, text
from sqlalchemy.exc import ArgumentError, InvalidRequestError


def test_redis_primary_key_ddl_does_not_emit_serial():
    from sqlalchemy import Column, Identity, Integer, MetaData, Table
    from sqlalchemy.exc import CompileError
    from sqlalchemy.schema import CreateTable
    from adbc_driver_redis.sqlalchemy import RedisDialect

    metadata = MetaData()
    explicit = Table("explicit_key", metadata, Column("id", Integer, primary_key=True))
    sql = str(CreateTable(explicit).compile(dialect=RedisDialect()))
    assert "SERIAL" not in sql and "id INTEGER NOT NULL" in sql
    identity = Table("identity_key", metadata, Column("id", Integer, Identity(), primary_key=True))
    with pytest.raises(CompileError, match="does not support identity"):
        CreateTable(identity).compile(dialect=RedisDialect())


def test_typed_bindings_returning_and_reflection(uri):
    pytest.importorskip("sqlalchemy", minversion="2.0")
    from sqlalchemy import Column, Float, Integer, LargeBinary, MetaData, Numeric, Table, Uuid, select
    from uuid import uuid4

    table = Table("it_sa_" + uuid4().hex, MetaData(),
                  Column("id", Integer, primary_key=True, autoincrement=False),
                  Column("amount", Numeric(15, 4)), Column("ratio", Float),
                  Column("payload", LargeBinary), Column("token", Uuid))
    engine = create_engine(uri.replace("://", "+adbc://", 1))
    token = UUID("12345678-1234-5678-1234-567812345678")
    try:
        with engine.connect() as conn:
            table.create(conn)
            try:
                conn.execute(table.insert(), [
                    dict(id=1, amount=1.25, ratio=Decimal("2.5"), payload=b"\x00\xff", token=token),
                    dict(id=2, amount=Decimal("3.1250"), ratio=4.0, payload=b"", token=None),
                ])
                rows = conn.execute(select(table).order_by(table.c.id)).all()
                assert rows[0] == (1, Decimal("1.2500"), 2.5, b"\x00\xff", token)
                assert rows[1] == (2, Decimal("3.1250"), 4.0, b"", None)
                assert conn.execute(table.update().where(table.c.id == 1).values(amount=Decimal("7.5")).returning(table.c.amount)).scalar_one() == Decimal("7.5000")
                assert conn.execute(table.delete().where(table.c.id == 2).returning(table.c.id)).scalar_one() == 2
                assert conn.execute(table.insert().values(id=3).returning(table.c.id)).scalar_one() == 3
                assert inspect(conn).get_table_options(table.name) == {}
                reflected = Table(table.name, MetaData(), autoload_with=conn)
                assert list(reflected.columns.keys()) == list(table.columns.keys())
            finally:
                table.drop(conn)
    finally:
        engine.dispose()


def test_decimal_division_with_bound_nullif(uri):
    from sqlalchemy import Numeric, cast, func, literal_column, select

    engine = create_engine(uri.replace("://", "+adbc://", 1))
    try:
        with engine.connect() as conn:
            for left, right, typ, expected in [
                ("1", "2.00", Numeric(10, 2), Decimal("0.5")),
                ("0.00000000000049382716", "0.00000000000012345679", Numeric(38, 20), Decimal("4")),
                ("30000000000000000000000000000000000000", "10000000000000000000000000000000000000", Numeric(38, 0), Decimal("3")),
            ]:
                denominator = func.nullif(cast(literal_column(right), typ), 0)
                assert conn.scalar(select(cast(literal_column(left), typ) / denominator)) == expected
    finally:
        engine.dispose()


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
