"""Compatibility with the community resource, independent of our IO manager."""

import uuid
import warnings

import pyarrow as pa
import pytest
from adbc_driver_redis import _driver_path, dbapi
from adbc_driver_redis.manifest import install_manifest
from dagster import Definitions, asset, materialize
from dagster_adbc import ADBCResource


@pytest.fixture(params=["library", "manifest"])
def resource(request, uri, tmp_path, monkeypatch):
    schema = "it_dagster_adbc_" + uuid.uuid4().hex
    driver = _driver_path()
    if request.param == "manifest":
        install_manifest(driver, directory=tmp_path)
        monkeypatch.setenv("ADBC_DRIVER_PATH", str(tmp_path))
        driver = "redis"
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute(f'CREATE SCHEMA "{schema}"')
    try:
        yield ADBCResource(
            driver=driver,
            uri=uri,
            autocommit=True,
            db_kwargs={"adbc.redis.default_schema": schema},
        )
    finally:
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            cur.execute(f'DROP SCHEMA IF EXISTS "{schema}" CASCADE')


def test_arrow_ingest_and_bound_query(resource):
    data = pa.table(
        {"id": [1, 2, 3], "name": ["O'Reilly", None, "雪"], "amount": [10, 20, 30]}
    )
    with warnings.catch_warnings():
        warnings.simplefilter("error")
        with resource.get_connection() as conn, conn.cursor() as cur:
            assert cur.adbc_ingest("orders", data, mode="create") == 3
            cur.execute("SELECT * FROM orders ORDER BY id")
            assert cur.fetch_arrow_table().to_pylist() == data.to_pylist()
            cur.execute("SELECT id FROM orders WHERE name = ?", ("O'Reilly",))
            assert cur.fetchall() == [(1,)]
    # The configured default schema and persisted writes survive reconnection.
    with resource.get_connection() as conn, conn.cursor() as cur:
        cur.execute("SELECT sum(amount) FROM orders")
        assert cur.fetchone() == (60,)


def test_materialize_resource_dependencies(resource, tmp_path):
    @asset
    def community_source(redis: ADBCResource) -> int:
        with redis.get_connection() as conn, conn.cursor() as cur:
            cur.adbc_ingest("asset_rows", pa.table({"id": [1, 2]}), mode="create")
        return 2

    @asset
    def community_summary(redis: ADBCResource, community_source: int) -> int:
        with redis.get_connection() as conn, conn.cursor() as cur:
            cur.execute("SELECT count(*) FROM asset_rows")
            count = cur.fetchone()[0]
        assert count == community_source
        return count

    assets = [community_source, community_summary]
    definitions = Definitions(assets=assets, resources={"redis": resource})
    Definitions.validate_loadable(definitions)
    result = materialize(
        assets,
        resources={"redis": resource},
        run_config={"resources": {"io_manager": {"config": {"base_dir": str(tmp_path)}}}},
    )
    assert result.success
    assert result.output_for_node("community_summary") == 2


def test_connection_closed_after_asset_error(resource):
    with pytest.raises(RuntimeError, match="injected asset failure"):
        with resource.get_connection() as conn:
            raise RuntimeError("injected asset failure")
    with pytest.raises(dbapi.ProgrammingError):
        conn.cursor()
    with resource.get_connection() as reopened, reopened.cursor() as cur:
        cur.execute("SELECT 1")
        assert cur.fetchone() == (1,)
