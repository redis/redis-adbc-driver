"""Airflow 3 TaskFlow smoke DAG using Redis ADBC directly.

Set REDIS_URI to a disposable Redis database with Search. Install the driver
and PyArrow in every Airflow worker. Only small summaries go through XCom.
"""

from datetime import datetime, timezone

from airflow.sdk import dag, get_current_context, task


def table_name():
    import hashlib

    # A stable name across retries, isolated from other DAG runs.
    run_id = get_current_context()["run_id"]
    suffix = hashlib.sha256(run_id.encode()).hexdigest()[:24]
    return f"airflow_adbc_smoke_{suffix}"


@dag(
    dag_id="redis_adbc_smoke",
    schedule=None,
    start_date=datetime(2026, 1, 1, tzinfo=timezone.utc),
    catchup=False,
    max_active_runs=1,
    tags=["redis", "adbc"],
)
def redis_adbc_smoke():
    @task
    def load():
        import os

        import pyarrow as pa
        from adbc_driver_redis import dbapi

        table = table_name()
        with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
            # Autocommit only. Replacing this run's fixture makes retries safe.
            cur.execute(f"DROP TABLE IF EXISTS {table}")
            cur.adbc_ingest(
                table,
                pa.table({
                    "id": [1, 2, 3, 4],
                    "region": ["US", "EU", "US", "EU"],
                    "amount": [10, 20, 30, 40],
                }),
                mode="create",
            )
        return {"table": table, "rows": 4}

    @task
    def verify(loaded):
        import os

        from adbc_driver_redis import dbapi

        table = table_name()
        assert loaded == {"table": table, "rows": 4}, loaded
        # A fresh task-local connection reads the prior task's persisted data.
        with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
            cur.execute(
                f"SELECT region, COUNT(*) AS n, SUM(amount) AS total "
                f"FROM {table} GROUP BY region ORDER BY region"
            )
            rows = cur.fetch_arrow_table().to_pylist()
            expected = [
                {"region": "EU", "n": 2, "total": 60},
                {"region": "US", "n": 2, "total": 40},
            ]
            assert rows == expected, rows
            cur.execute(f"SELECT id FROM {table} WHERE amount > ? ORDER BY id", [20])
            assert cur.fetchall() == [(3,), (4,)]
        print(f"Redis ADBC verified: {loaded['rows']} rows; aggregates={rows}")
        return {"rows": loaded["rows"], "groups": len(rows)}

    @task(trigger_rule="all_done")
    def cleanup():
        import os

        from adbc_driver_redis import dbapi

        with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
            cur.execute(f"DROP TABLE IF EXISTS {table_name()}")

    loaded = load()
    checked = verify(loaded)
    # Cleanup does not depend on a successful XCom result.
    [loaded, checked] >> cleanup()


dag = redis_adbc_smoke()

if __name__ == "__main__":
    run = dag.test()
    states = {ti.task_id: str(ti.state) for ti in run.get_task_instances()}
    # An all_done leaf can hide upstream failure in the overall DagRun state.
    assert states == {"load": "success", "verify": "success", "cleanup": "success"}, states
    print(f"Redis ADBC Airflow smoke passed: {states}")
