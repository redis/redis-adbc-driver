"""Execute local components/tasks/SDK syncs with task-local native ADBC reads."""

import os
from pathlib import Path
import subprocess
import sys
import textwrap

import pytest


def test_kubeflow_local_component(uri, workflow_rows, tmp_path, monkeypatch):
    pytest.importorskip("kfp")
    from kfp import dsl, local

    monkeypatch.setenv("PATH", str(Path(sys.executable).parent) + os.pathsep + os.environ["PATH"])
    name, _ = workflow_rows
    local.init(runner=local.SubprocessRunner(use_venv=False), pipeline_root=str(tmp_path / "outputs"))

    @dsl.component(base_image="python:3.12", install_kfp_package=False)
    def read_redis(redis_uri: str, table_name: str) -> int:
        from adbc_driver_redis import dbapi

        with dbapi.connect(redis_uri) as conn, conn.cursor() as cur:
            cur.execute(f'SELECT * FROM "{table_name}" ORDER BY id')
            rows = cur.fetch_arrow_table().to_pylist()
        assert [r["label"] for r in rows] == ["alpha", None, "雪"]
        assert [r["amount"] for r in rows] == [2.5, None, 7.5]
        return sum(r["id"] for r in rows)

    task = read_redis(redis_uri=uri, table_name=name)
    assert task.output == 6


def test_metaflow_local_flow(uri, workflow_rows, tmp_path):
    pytest.importorskip("metaflow")
    name, _ = workflow_rows
    flow = tmp_path / "flow.py"
    flow.write_text(textwrap.dedent("""\
        import os
        from metaflow import FlowSpec, step

        class RedisADBCCandidateFlow(FlowSpec):
            @step
            def start(self):
                from adbc_driver_redis import dbapi
                with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
                    name = os.environ["REDIS_ADBC_TEST_TABLE"]
                    cur.execute(f'SELECT * FROM "{name}" ORDER BY id')
                    self.rows = cur.fetch_arrow_table().to_pylist()
                self.next(self.end)

            @step
            def end(self):
                assert [r["id"] for r in self.rows] == [1, 2, 3]
                assert [r["label"] for r in self.rows] == ["alpha", None, "雪"]
                assert [r["active"] for r in self.rows] == [True, None, False]
                print("REDIS_ADBC_METAFLOW_OK")

        if __name__ == "__main__":
            RedisADBCCandidateFlow()
        """))
    env = {
        **os.environ, "REDIS_URI": uri, "REDIS_ADBC_TEST_TABLE": name,
        "METAFLOW_USER": "redis_adbc_test", "METAFLOW_DEFAULT_DATASTORE": "local",
        "METAFLOW_DEFAULT_METADATA": "local", "METAFLOW_HOME": str(tmp_path / "config"),
        "METAFLOW_DATASTORE_SYSROOT_LOCAL": str(tmp_path / ".metaflow"),
    }
    result = subprocess.run(
        [sys.executable, str(flow), "--no-pylint", "run"], cwd=tmp_path, env=env,
        text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180,
    )
    assert result.returncode == 0, result.stdout
    assert "REDIS_ADBC_METAFLOW_OK" in result.stdout
    assert (tmp_path / ".metaflow").exists()


def test_fivetran_sdk_local_tester(uri, workflow_rows, tmp_path):
    sdk = pytest.importorskip("fivetran_connector_sdk")
    duckdb = pytest.importorskip("duckdb")
    name, expected = workflow_rows
    script = tmp_path / "connector.py"
    script.write_text(textwrap.dedent("""\
        import os
        from adbc_driver_redis import dbapi
        from fivetran_connector_sdk import Connector, Operations as op
        from fivetran_connector_sdk import connector_helper

        # Redirect downloaded tester artifacts only; use the real SDK and tester.
        connector_helper.ROOT_LOCATION = os.environ["REDIS_ADBC_SDK_CACHE"]

        def schema(configuration):
            return [{"table": "rows", "primary_key": ["id"], "columns": {
                "id": "LONG", "label": "STRING", "amount": "DOUBLE", "active": "BOOLEAN"
            }}]

        def update(configuration, state):
            with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
                name = os.environ["REDIS_ADBC_TEST_TABLE"]
                cur.execute(f'SELECT * FROM "{name}" ORDER BY id')
                for row in cur.fetch_arrow_table().to_pylist():
                    op.upsert(table="rows", data=row)
            op.checkpoint(state={"complete": True})

        connector = Connector(update=update, schema=schema)
        if __name__ == "__main__":
            connector.debug(project_path=os.path.dirname(__file__), configuration={})
        """))
    (tmp_path / "requirements.txt").write_text(
        f"fivetran-connector-sdk=={sdk.__version__}\nadbc-driver-manager\npyarrow\n"
    )
    env = {**os.environ, "REDIS_URI": uri, "REDIS_ADBC_TEST_TABLE": name,
           "REDIS_ADBC_SDK_CACHE": os.environ.get("CANDIDATE_SDK_CACHE", str(tmp_path / "sdk-tools")),
           "PATH": str(Path(sys.executable).parent) + os.pathsep + os.environ["PATH"]}
    result = subprocess.run(
        [sys.executable, str(script)], cwd=tmp_path, env=env, text=True,
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=300,
    )
    assert result.returncode == 0, result.stdout
    assert "SYNC SUCCEEDED" in result.stdout, result.stdout
    with duckdb.connect(str(tmp_path / "files" / "warehouse.db"), read_only=True) as warehouse:
        tables = warehouse.execute(
            "SELECT table_schema FROM information_schema.tables WHERE table_name='rows'"
        ).fetchall()
        assert len(tables) == 1
        schema = tables[0][0].replace('"', '""')
        actual = warehouse.execute(
            f'SELECT id, label, amount, active FROM "{schema}"."rows" ORDER BY id'
        ).fetchall()
        assert actual == [tuple(row.values()) for row in expected.to_pylist()]
