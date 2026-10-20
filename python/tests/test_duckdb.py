"""Run explicitly: pytest --duckdb would be ambiguous; use RUN_DUCKDB_TESTS=1.

The community extension is downloaded on demand; this test does not silently
skip missing extensions or failed network access once explicitly enabled.
"""

import os

import pytest

pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_DUCKDB_TESTS") != "1",
    reason="set RUN_DUCKDB_TESTS=1 to download/load the community extension",
)


def test_duckdb_read_join_ingest_execute_and_transaction_rejection(
    uri, connection, tmp_path, monkeypatch
):
    import duckdb
    from adbc_driver_redis import _driver_path
    from adbc_driver_redis.manifest import install_manifest

    install_manifest(_driver_path(), directory=tmp_path)
    monkeypatch.setenv("ADBC_DRIVER_PATH", str(tmp_path))
    monkeypatch.setenv("QUERY_FARM_TELEMETRY_OPT_OUT", "1")
    db = duckdb.connect(config={"extension_directory": str(tmp_path / "extensions")})

    def literal(s):
        return "'" + s.replace("'", "''") + "'"

    try:
        db.execute("INSTALL adbc_scanner FROM community")
        db.execute("LOAD adbc_scanner")
        db.execute(f"ATTACH {literal(uri)} AS r (TYPE adbc, driver 'redis')")
        db.execute(
            "CALL adbc_execute('r', 'DROP TABLE IF EXISTS it_python_duckdb')"
        ).fetchall()
        try:
            inserted = db.execute(
                "SELECT * FROM adbc_insert('r', 'it_python_duckdb', (SELECT * FROM (VALUES (1, 'US'), (2, 'CA')) v(id,country)), mode := 'create')"
            ).fetchall()
            assert inserted
            assert db.execute("SELECT count(*) FROM r.it_python_duckdb").fetchall() == [
                (2,)
            ]
            assert db.execute(
                "SELECT * FROM r.public.it_python_duckdb ORDER BY id"
            ).fetchall() == [(1, "US"), (2, "CA")]
            assert db.execute(
                "SELECT country, label FROM r.public.it_python_duckdb JOIN (VALUES (1,'one'),(2,'two')) x(k,label) ON id=k ORDER BY id"
            ).fetchall() == [("US", "one"), ("CA", "two")]
            assert db.execute(
                "SELECT * FROM adbc_scan('r', 'SELECT count(*) AS n FROM it_python_duckdb WHERE id=1')"
            ).fetchall() == [(1,)]
            db.execute(
                "CALL adbc_execute('r', 'INSERT INTO it_python_duckdb VALUES (3, ''GB'')')"
            ).fetchall()
            assert db.execute(
                "SELECT count(*) FROM r.public.it_python_duckdb"
            ).fetchall() == [(3,)]
            # Current adbc_scanner supports implicit autocommit catalog writes
            # for this driver. Explicit transactions must still be rejected.
            db.execute(
                "INSERT INTO r.public.it_python_duckdb VALUES (4,'FR')"
            ).fetchall()
            assert db.execute(
                "SELECT count(*) FROM r.public.it_python_duckdb"
            ).fetchall() == [(4,)]
            db.execute(
                "CREATE TABLE r.public.it_python_duckdb_ctas AS SELECT 9 AS id"
            ).fetchall()
            assert db.execute(
                "SELECT * FROM r.public.it_python_duckdb_ctas"
            ).fetchall() == [(9,)]
            for statement in (
                "INSERT INTO r.public.it_python_duckdb VALUES (5,'DE')",
                "CALL adbc_execute('r', 'INSERT INTO it_python_duckdb VALUES (5, ''DE'')')",
                "SELECT * FROM adbc_insert('r', 'it_python_duckdb', (SELECT 5 AS id, 'DE' AS country), mode := 'append')",
            ):
                db.execute("BEGIN")
                with pytest.raises(duckdb.Error, match="(?i)autocommit|transaction"):
                    db.execute(statement).fetchall()
                db.execute("ROLLBACK")
                assert db.execute(
                    "SELECT count(*) FROM r.public.it_python_duckdb"
                ).fetchall() == [(4,)]
            plan = db.execute(
                "EXPLAIN SELECT country FROM r.public.it_python_duckdb WHERE id=1"
            ).fetchall()
            print("DuckDB filter plan:", plan)
            db.execute("DETACH r")
            db.execute(
                f"ATTACH {literal(uri)} AS r (TYPE adbc, driver 'redis', READ_ONLY)"
            )
            assert db.execute(
                "SELECT count(*) FROM r.public.it_python_duckdb"
            ).fetchall() == [(4,)]
            with pytest.raises(duckdb.Error, match="(?i)read.only"):
                db.execute(
                    "CALL adbc_execute('r', 'DELETE FROM it_python_duckdb')"
                ).fetchall()
        finally:
            with connection.cursor() as cur:
                cur.execute("DROP TABLE IF EXISTS it_python_duckdb")
                cur.execute("DROP TABLE IF EXISTS it_python_duckdb_ctas")
    finally:
        db.close()
