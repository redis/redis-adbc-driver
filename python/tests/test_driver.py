import warnings

import adbc_driver_manager.dbapi as manager
import pytest
from adbc_driver_redis import dbapi
from adbc_driver_redis.manifest import install_manifest


def test_dbapi_and_polars(uri):
    import polars as pl

    with warnings.catch_warnings():
        warnings.simplefilter("error")
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            cur.execute("SELECT 1 AS one")
            assert cur.fetchall() == [(1,)]
        assert pl.read_database_uri(
            "SELECT 1 AS one", uri, engine="adbc"
        ).to_dicts() == [{"one": 1}]
    from adbc_driver_rediss.dbapi import connect

    assert connect is dbapi.connect


def test_autocommit_rejected():
    with pytest.raises(manager.NotSupportedError, match="autocommit=True"):
        dbapi.connect("redis://localhost:1/0", autocommit=False)


def test_manifest_discovery(uri, tmp_path, monkeypatch):
    from adbc_driver_redis import _driver_path

    paths = install_manifest(_driver_path(), directory=tmp_path)
    assert [p.name for p in paths] == ["redis.toml", "rediss.toml"]
    monkeypatch.setenv("ADBC_DRIVER_PATH", str(tmp_path))
    for driver in (None, "redis", str(paths[0])):
        with (
            manager.connect(driver=driver, uri=uri, autocommit=True) as conn,
            conn.cursor() as cur,
        ):
            cur.execute("SELECT 1")
            assert cur.fetchone() == (1,)
    # Validate that TLS scheme discovery reaches this driver, without needing
    # a TLS server or accepting an insecure connection.
    assert paths[0].read_text() == paths[1].read_text()


def test_collation(connection):
    with connection.cursor() as cur:
        cur.execute("DROP TABLE IF EXISTS it_python_collation")
        try:
            cur.execute('CREATE TABLE it_python_collation (name text COLLATE "C")')
            cur.execute(
                "INSERT INTO it_python_collation VALUES ('apple'), ('Banana'), ('cherry'), ('_x')"
            )
            cur.execute(
                'SELECT name FROM it_python_collation ORDER BY name COLLATE "C"'
            )
            assert cur.fetchall() == [("Banana",), ("_x",), ("apple",), ("cherry",)]
            cur.execute(
                "SELECT collation_name FROM information_schema.columns WHERE table_name='it_python_collation'"
            )
            assert cur.fetchone() == ("C",)
            with pytest.raises(manager.Error) as error:
                cur.execute('SELECT name COLLATE "missing" FROM it_python_collation')
            assert error.value.sqlstate == "42704"
        finally:
            cur.execute("DROP TABLE it_python_collation")
