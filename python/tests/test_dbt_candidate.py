"""Build, test, and document a small project with the existing native dbt adapter."""

import os
from pathlib import Path
import subprocess
import sys
from uuid import uuid4

import pytest
from adbc_driver_redis import dbapi


def test_native_dbt_build_and_catalog(uri, tmp_path):
    pytest.importorskip("dbt.adapters.redis_adbc")
    schema = "it_dbt_candidate_" + uuid4().hex
    (tmp_path / "models").mkdir()
    (tmp_path / "seeds").mkdir()
    (tmp_path / "dbt_project.yml").write_text(
        "name: redis_candidate\nversion: '1.0'\nconfig-version: 2\nprofile: redis_candidate\n"
        "model-paths: [models]\nseed-paths: [seeds]\n"
    )
    (tmp_path / "profiles.yml").write_text(
        "redis_candidate:\n  target: dev\n  outputs:\n    dev:\n"
        "      type: redis_adbc\n      database: redis\n"
        f"      schema: {schema}\n      threads: 1\n"
        "      driver: \"{{ env_var('REDIS_ADBC_LIBRARY') }}\"\n"
        "      uri: \"{{ env_var('REDIS_URI') }}\"\n"
    )
    (tmp_path / "seeds" / "input.csv").write_text("id,label,amount\n1,alpha,10\n2,,20\n3,雪,30\n")
    (tmp_path / "models" / "result.sql").write_text(
        "{{ config(materialized='table') }}\n"
        "SELECT id, label, amount * 2 AS doubled FROM {{ ref('input') }} WHERE id >= 2\n"
    )
    (tmp_path / "models" / "schema.yml").write_text(
        "version: 2\nmodels:\n  - name: result\n    columns:\n"
        "      - name: id\n        tests: [not_null, unique]\n"
    )
    env = {**os.environ, "REDIS_URI": uri, "DBT_SEND_ANONYMOUS_USAGE_STATS": "false",
           "DBT_PARTIAL_PARSE": "false"}
    executable = str(Path(sys.executable).parent / "dbt")
    try:
        for command in (["build"], ["docs", "generate"]):
            result = subprocess.run(
                [executable, *command, "--project-dir", str(tmp_path), "--profiles-dir", str(tmp_path)],
                cwd=tmp_path, env=env, text=True, stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT, timeout=180,
            )
            assert result.returncode == 0, result.stdout
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            cur.execute(f'SELECT id, label, doubled FROM "{schema}".result ORDER BY id')
            assert cur.fetchall() == [(2, None, 40), (3, "雪", 60)]
        assert (tmp_path / "target" / "catalog.json").exists()
    finally:
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            cur.execute(f'DROP SCHEMA IF EXISTS "{schema}" CASCADE')
