"""Real Prefect orchestration with task-local Redis ADBC connections."""

from uuid import uuid4

import pyarrow as pa
import pytest
from adbc_driver_redis import dbapi

pytest.importorskip("prefect")
from prefect import flow, task
from prefect.cache_policies import NO_CACHE
from prefect.context import get_run_context
from prefect.task_runners import ThreadPoolTaskRunner
from prefect.testing.utilities import prefect_test_harness


@pytest.fixture(scope="module")
def prefect_backend():
    with prefect_test_harness():
        yield


@pytest.fixture
def table_name(uri, prefect_backend):
    name = "it_prefect_" + uuid4().hex
    yield name
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {name}")


@task(cache_policy=NO_CACHE, persist_result=False)
def load_rows(uri, name):
    data = pa.table({"id": [1, 2, 3], "label": ["a", None, "雪"], "n": [10, 20, 30]})
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.adbc_ingest(name, data, mode="create")
    return name


@task(cache_policy=NO_CACHE, persist_result=False)
def read_rows(uri, name):
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute(f"SELECT id, label, n FROM {name} ORDER BY id")
        return cur.fetch_arrow_table()


def test_flow_arrow_roundtrip(uri, table_name):
    @flow(persist_result=False)
    def pipeline():
        return read_rows.submit(uri, load_rows.submit(uri, table_name)).result()

    result = pipeline()
    assert result.to_pylist() == [
        {"id": 1, "label": "a", "n": 10},
        {"id": 2, "label": None, "n": 20},
        {"id": 3, "label": "雪", "n": 30},
    ]
    assert result.schema == pa.schema(
        [("id", pa.int64()), ("label", pa.string()), ("n", pa.int64())]
    )


def test_concurrent_queries(uri, table_name):
    @task(cache_policy=NO_CACHE, persist_result=False)
    def total(name):
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            cur.execute(f"SELECT sum(n) FROM {name}")
            return cur.fetchone()[0]

    @flow(task_runner=ThreadPoolTaskRunner(max_workers=4), persist_result=False)
    def pipeline():
        loaded = load_rows.submit(uri, table_name)
        futures = [total.submit(loaded) for _ in range(4)]
        return [future.result() for future in futures]

    assert pipeline() == [60] * 4


def test_retry_after_committed_write(uri, table_name):
    attempts = []

    @task(retries=1, retry_delay_seconds=0, cache_policy=NO_CACHE, persist_result=False)
    def replace_rows():
        attempt = get_run_context().task_run.run_count
        attempts.append(attempt)
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            # Replacement makes this task retry-safe: Redis writes autocommit.
            cur.adbc_ingest(table_name, pa.table({"id": [1, 2]}), mode="replace")
        if attempt == 1:
            raise RuntimeError("injected failure after committed write")
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            cur.execute(f"SELECT id FROM {table_name} ORDER BY id")
            return cur.fetchall()

    @flow(persist_result=False)
    def pipeline():
        return replace_rows()

    assert pipeline() == [(1,), (2,)]
    assert attempts == [1, 2]


def test_driver_error_fails_flow(uri, table_name):
    @task(cache_policy=NO_CACHE, persist_result=False)
    def missing_table():
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            cur.execute(f"SELECT * FROM {table_name}")

    @flow(persist_result=False)
    def pipeline():
        missing_table()

    state = pipeline(return_state=True)
    assert state.is_failed()
    with pytest.raises(dbapi.Error) as error:
        state.result()
    assert error.value.sqlstate == "42P01"
    # A failed task must release its connection and leave the driver usable.
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute("SELECT 1")
        assert cur.fetchall() == [(1,)]
