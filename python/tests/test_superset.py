"""Optional smoke test in a separate environment with apache-superset 6.1.

Set RUN_SUPERSET_TESTS=1. Uses a temporary metadata SQLite database, never an
existing Superset installation. Requires the sqlalchemy extra and pytest.
"""

import os

import pytest

pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_SUPERSET_TESTS") != "1",
    reason="requires isolated Superset environment and RUN_SUPERSET_TESTS=1",
)


def test_superset_dataset_and_chart_queries(uri, tmp_path, monkeypatch):
    config = tmp_path / "superset_config.py"
    config.write_text(
        "SECRET_KEY = 'disposable-local-integration-only'\n"
        f"SQLALCHEMY_DATABASE_URI = 'sqlite:///{tmp_path}/metadata.db'\n"
        "WTF_CSRF_ENABLED = False\nTALISMAN_ENABLED = False\n"
        "CONTENT_SECURITY_POLICY_WARNING = False\nTESTING = True\n"
        "CACHE_CONFIG = {'CACHE_TYPE': 'SimpleCache'}\n"
        "DATA_CACHE_CONFIG = {'CACHE_TYPE': 'SimpleCache'}\n"
    )
    monkeypatch.setenv("SUPERSET_CONFIG_PATH", str(config))
    monkeypatch.setenv("SUPERSET_HOME", str(tmp_path))
    from superset.app import create_app

    app = create_app()
    with app.app_context(), app.test_request_context():
        from flask import g
        from flask_login import login_user
        from sqlglot.dialects.dialect import Dialects
        from superset import db, security_manager
        from superset.connectors.sqla.models import SqlaTable
        from superset.db_engine_specs import get_engine_spec
        from superset.models.core import Database
        from superset.sql.parse import SQLGLOT_DIALECTS

        assert get_engine_spec("redis", "adbc").engine == "redis"
        assert get_engine_spec("rediss", "adbc").engine == "rediss"
        assert SQLGLOT_DIALECTS["redis"] == Dialects.POSTGRES
        db.create_all()
        from flask_appbuilder import Model

        Model.metadata.create_all(db.engine)
        role = security_manager.add_role("Admin")
        user = security_manager.add_user(
            "test",
            "Test",
            "User",
            "test@example.invalid",
            role,
            password="local-test-only",
        )
        login_user(user)
        g.user = user
        database = Database(
            database_name="Redis integration",
            sqlalchemy_uri=uri.replace("://", "+adbc://", 1),
            expose_in_sqllab=True,
        )
        db.session.add(database)
        db.session.commit()
        with database.get_sqla_engine() as engine, engine.connect() as conn:
            assert conn.exec_driver_sql("SELECT 1").scalar() == 1
            conn.exec_driver_sql("CREATE SCHEMA it_python_superset")
            conn.exec_driver_sql(
                "CREATE TABLE it_python_superset.sales (id integer, ts timestamp, country text, amount integer)"
            )
            conn.exec_driver_sql(
                "INSERT INTO it_python_superset.sales VALUES (1,TIMESTAMP '2026-01-01 01:00:00','US',10),(2,TIMESTAMP '2026-01-01 02:00:00','US',20),(3,TIMESTAMP '2026-01-02','CA',5)"
            )
        try:
            assert "it_python_superset" in database.get_all_schema_names()
            assert any(
                row[0] == "sales"
                for row in database.get_all_table_names_in_schema(
                    catalog=None, schema="it_python_superset"
                )
            )
            dataset = SqlaTable(
                table_name="sales", schema="it_python_superset", database=database
            )
            dataset.fetch_metadata()
            db.session.add(dataset)
            db.session.commit()
            assert {c.column_name for c in dataset.columns} == {
                "id",
                "ts",
                "country",
                "amount",
            }
            metric = {
                "expressionType": "SQL",
                "sqlExpression": "SUM(amount)",
                "label": "total",
                "optionName": "metric_total",
            }
            common = {
                "metrics": [metric],
                "filter": [],
                "extras": {},
                "row_limit": 100,
                "orderby": [],
                "is_timeseries": False,
                "columns": ["country"],
            }
            result = dataset.query(common)
            assert result.error_message is None, result.error_message
            assert dict(zip(result.df["country"], result.df["total"])) == {
                "US": 30,
                "CA": 5,
            }
            timeseries = {
                **common,
                "is_timeseries": True,
                "columns": [],
                "granularity": "ts",
                "extras": {"time_grain_sqla": "P1D"},
            }
            result = dataset.query(timeseries)
            assert result.error_message is None, result.error_message
            assert sorted(result.df["total"].tolist()) == [5, 30]
            # SQL Lab executes through the spec's raw DB-API cursor path.
            with database.get_raw_connection() as raw, raw.cursor() as cursor:
                database.db_engine_spec.execute(
                    cursor,
                    "SELECT country, SUM(amount) FROM it_python_superset.sales GROUP BY country ORDER BY country",
                    database,
                )
                assert cursor.fetchall() == [("CA", 5), ("US", 30)]
        finally:
            with database.get_sqla_engine() as engine, engine.connect() as conn:
                conn.exec_driver_sql("DROP SCHEMA it_python_superset CASCADE")
            db.session.remove()
