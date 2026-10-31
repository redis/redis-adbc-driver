"""Execute linked examples and non-Python instructions from integration guides."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


def run(command, directory, **kwargs):
    return subprocess.run(command, cwd=directory, check=True, timeout=300, **kwargs)


def check_linked_workflow(tool, root):
    with tempfile.TemporaryDirectory(prefix=f"redis-guide-{tool}-") as directory:
        work = Path(directory)
        env = {**os.environ}
        if tool in {"dagster", "dask"}:
            filename = "dagster_smoke.py" if tool == "dagster" else "dask_read.py"
            run([sys.executable, str(root / "python/examples" / filename)], work)
        elif tool == "airflow":
            # Execute the exact hook block with the connection configured as the guide describes.
            env["AIRFLOW_CONN_REDIS_ANALYTICS"] = json.dumps({"conn_type": "adbc", "extra": {
                "dialect": "redis", "db_kwargs": {"uri": env["REDIS_URI"]}}})
            env["AIRFLOW_HOME"] = str(work / "airflow")
            guide = (root / "docs/integrations/airflow.md").read_text()
            code = re.findall(r"```python\n(.*?)```", guide, re.DOTALL)[0]
            script = work / "hook.py"
            script.write_text(code)
            run([sys.executable, str(script)], work, env=env)
            # The guide's runnable DAG is invoked by its documented test command.
            env["RUN_AIRFLOW_TESTS"] = "1"
            run([sys.executable, "-m", "pytest", "-q", str(root / "python/tests/test_airflow.py")], work, env=env)
        elif tool == "superset":
            env["RUN_SUPERSET_TESTS"] = "1"
            run([sys.executable, "-m", "pytest", "-q", str(root / "python/tests/test_superset.py")], work, env=env)
        elif tool == "dbt":
            check_dbt(root, work, env)
        elif tool == "duckdb":
            check_duckdb(root, work)
        else:
            raise ValueError(tool)


def check_dbt(root, work, env):
    text = (root / "docs/integrations/dbt.md").read_text()
    for name in ("dbt_project.yml", "profiles.yml", "seeds/orders.csv", "models/revenue.sql", "tests/revenue_is_42.sql"):
        match = re.search(r"Save `" + re.escape(name) + r"`[^\n]*[\s\S]*?```\w+\n(.*?)```", text, re.DOTALL)
        if match is None:
            raise ValueError(f"Missing starter-project block for {name}")
        path = work / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(match[1])
    from adbc_driver_redis import _driver_path, dbapi
    # Keep the guide's schema name, but refuse to touch any pre-existing schema.
    with dbapi.connect(env["REDIS_URI"]) as conn, conn.cursor() as cur:
        cur.execute("SELECT schema_name FROM information_schema.schemata WHERE schema_name='dbt_guide'")
        if cur.fetchall():
            raise RuntimeError("dbt_guide already exists; use a dedicated clean database")
    env["REDIS_ADBC_DRIVER"] = _driver_path()
    try:
        for command in (["debug"], ["seed"], ["build"], ["docs", "generate"],
                        ["show", "--inline", "select * from {{ ref('revenue') }}", "--output", "json"]):
            run(["dbt", *command, "--profiles-dir", "."], work, env=env)
        with dbapi.connect(env["REDIS_URI"]) as conn, conn.cursor() as cur:
            cur.execute("SELECT order_count, total FROM dbt_guide.revenue")
            assert cur.fetchall() == [(3, 42)]
        assert (work / "target/catalog.json").is_file()
    finally:
        code = re.findall(r"```python\n(.*?)```", text, re.DOTALL)[0]
        cleanup = work / "cleanup.py"
        cleanup.write_text(code)
        run([sys.executable, str(cleanup)], work, env=env)


def check_duckdb(root, work):
    import duckdb
    from adbc_driver_redis import _driver_path, dbapi
    from adbc_driver_redis.manifest import install_manifest

    install_manifest(_driver_path(), directory=work / "manifests")
    os.environ["ADBC_DRIVER_PATH"] = str(work / "manifests")
    os.environ["QUERY_FARM_TELEMETRY_OPT_OUT"] = "1"
    text = (root / "docs/duckdb.md").read_text()
    blocks = re.findall(r"```sql\n(.*?)```", text, re.DOTALL)[:2]
    with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
        cur.execute("SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('orders', 'duckdb_orders')")
        if cur.fetchall():
            raise RuntimeError("DuckDB guide tables already exist; use a dedicated clean database")
        cur.execute("CREATE TABLE public.orders (id INTEGER, country TEXT)")
        cur.execute("INSERT INTO public.orders VALUES (1,'US'), (2,'CA')")
        try:
            with duckdb.connect(config={"extension_directory": str(work / "extensions")}) as db:
                for block in blocks:
                    db.execute(block).fetchall()
                cur.execute("SELECT id, country FROM public.duckdb_orders ORDER BY id")
                assert cur.fetchall() == [(1, "US"), (2, "CA")]
        finally:
            cur.execute("DROP TABLE IF EXISTS public.duckdb_orders")
            cur.execute("DROP TABLE IF EXISTS public.orders")
