"""Opt-in integration with a disposable Airflow metadata DB and Redis fixture."""

import json
import os
from pathlib import Path
import subprocess
import sys
import uuid

import pytest


pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_AIRFLOW_TESTS") != "1",
    reason="Set RUN_AIRFLOW_TESTS=1 in a separate Airflow environment",
)


def test_taskflow_roundtrip(uri, tmp_path):
    dags = tmp_path / "dags"
    dags.mkdir()
    source = Path(__file__).resolve().parents[1] / "examples" / "airflow_redis_smoke.py"
    target = dags / source.name
    target.write_text(source.read_text())
    env = {
        **os.environ,
        "AIRFLOW_HOME": str(tmp_path),
        "AIRFLOW__DATABASE__SQL_ALCHEMY_CONN": f"sqlite:///{tmp_path}/airflow.db",
        "AIRFLOW__CORE__DAGS_FOLDER": str(dags),
        "AIRFLOW__CORE__LOAD_EXAMPLES": "False",
        "REDIS_URI": uri,
    }

    def execute(*args):
        result = subprocess.run(
            [sys.executable, *args], env=env, text=True,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180,
        )
        assert result.returncode == 0, result.stdout
        return result.stdout

    execute("-m", "airflow", "db", "migrate")
    # Repeat against the same metadata DB to exercise separate run namespaces.
    for _ in range(2):
        output = execute(str(target))
        assert "Redis ADBC Airflow smoke passed:" in output, output


def test_arrow_provider_autocommit(uri, tmp_path, monkeypatch):
    monkeypatch.setenv("AIRFLOW_HOME", str(tmp_path))
    monkeypatch.setenv("AIRFLOW__CORE__LOAD_EXAMPLES", "False")
    monkeypatch.setenv(
        "AIRFLOW__DATABASE__SQL_ALCHEMY_CONN", f"sqlite:///{tmp_path}/airflow.db"
    )
    from adbc_driver_redis import _driver_path
    from adbc_driver_redis.manifest import install_manifest
    from adbc_driver_manager import NotSupportedError
    from airflow.providers.apache.arrow.hooks.adbc import AdbcHook, fetch_all_handler

    # Provider 0.1.1 looks for a bundled .so, but not a bundled .dylib.
    # Its fallback name can be resolved through an ADBC manifest on either OS.
    manifest = install_manifest(_driver_path(), directory=tmp_path)[0]
    (tmp_path / "adbc_driver_redis.toml").write_text(manifest.read_text())
    monkeypatch.setenv("ADBC_DRIVER_PATH", str(tmp_path))
    monkeypatch.setenv("AIRFLOW_CONN_REDIS_ADBC", json.dumps({
        "conn_type": "adbc",
        "extra": {
            "driver": "adbc_driver_redis",
            "dialect": "redis",
            # Provider 0.1.1 merges this after its generated URI; preserve TLS,
            # credentials and URI options exactly as supplied by the caller.
            "db_kwargs": {"uri": uri},
        },
    }))
    hook = AdbcHook(adbc_conn_id="redis_adbc")
    table = f"airflow_adbc_hook_{uuid.uuid4().hex}"
    # get_conn() currently requests autocommit=False before run() sets True.
    with pytest.warns(Warning, match="Cannot disable autocommit"):
        try:
            hook.run(f"CREATE TABLE {table} (id BIGINT, amount BIGINT)", autocommit=True)
            hook.insert_rows(
                table, [(1, 10), (2, 20), (3, 30)],
                target_fields=["id", "amount"], commit_every=2, autocommit=True,
            )
            assert hook.run(
                f"SELECT COUNT(*), SUM(amount) FROM {table} WHERE amount > %s",
                parameters=[10], autocommit=True, handler=fetch_all_handler,
            ) == [(2, 50)]
        finally:
            hook.run(f"DROP TABLE IF EXISTS {table}", autocommit=True)
    # get_records() defaults to autocommit=False and is incompatible with Redis.
    with pytest.warns(Warning, match="Cannot disable autocommit"):
        with pytest.raises(NotSupportedError):
            hook.get_records("SELECT 42 AS answer")
