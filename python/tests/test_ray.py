"""Opt-in local Ray Data integration checks against disposable Redis with Search."""

import os
from functools import partial
from uuid import uuid4

import pytest

pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_RAY_TESTS") != "1",
    reason="set RUN_RAY_TESTS=1 to start a local Ray runtime",
)


@pytest.fixture(scope="module")
def ray_runtime():
    import ray

    ray.init(num_cpus=2, include_dashboard=False)
    try:
        yield ray
    finally:
        ray.shutdown()


@pytest.fixture
def ray_table(connection):
    table = "it_python_ray_" + uuid4().hex
    with connection.cursor() as cur:
        cur.execute(f"CREATE TABLE {table} (id BIGINT, label TEXT, amount DOUBLE)")
        try:
            yield table
        finally:
            cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_ray_sql_read_filter_aggregate(ray_runtime, uri, connection, ray_table):
    from adbc_driver_redis import dbapi

    with connection.cursor() as cur:
        cur.execute(
            f"INSERT INTO {ray_table} VALUES "
            "(1, 'alpha', 1.5), (2, 'beta', NULL), (3, 'alpha', 3.5)"
        )
    factory = partial(dbapi.connect, uri)
    dataset = ray_runtime.data.read_sql(
        f"SELECT * FROM {ray_table} WHERE id >= ?",
        factory,
        sql_params=(2,),
        override_num_blocks=1,
    )
    assert sorted(dataset.take_all(), key=lambda row: row["id"]) == [
        {"id": 2, "label": "beta", "amount": None},
        {"id": 3, "label": "alpha", "amount": 3.5},
    ]
    result = ray_runtime.data.read_sql(
        f"SELECT label, COUNT(*) AS n, SUM(amount) AS total "
        f"FROM {ray_table} GROUP BY label",
        factory,
        override_num_blocks=1,
    )
    assert sorted(result.take_all(), key=lambda row: row["label"]) == [
        {"label": "alpha", "n": 2, "total": 5.0},
        {"label": "beta", "n": 1, "total": None},
    ]


def test_ray_sql_write_roundtrip(ray_runtime, uri, connection, ray_table):
    from adbc_driver_redis import dbapi

    # More than Ray's 128-row executemany batch, supplied in two input blocks.
    rows = [{"id": i, "label": f"row-{i}", "amount": i / 2} for i in range(300)]
    dataset = ray_runtime.data.from_items(rows, override_num_blocks=2)
    dataset.write_sql(
        f"INSERT INTO {ray_table} (id, label, amount) VALUES (?, ?, ?)",
        partial(dbapi.connect, uri),
        concurrency=2,
    )
    with connection.cursor() as cur:
        cur.execute(f"SELECT id, label, amount FROM {ray_table} ORDER BY id")
        assert cur.fetchall() == [(i, f"row-{i}", i / 2) for i in range(300)]


def test_ray_arrow_read_and_transform(ray_runtime, connection, ray_table):
    import pyarrow as pa

    source = pa.table(
        {"id": [1, 2, 3], "label": ["a", None, "c"], "amount": [1.5, 2.5, None]}
    )
    with connection.cursor() as cur:
        cur.adbc_ingest(ray_table, source, mode="append")
        cur.execute(f"SELECT id, label, amount FROM {ray_table} ORDER BY id")
        arrow = cur.fetch_arrow_table()
    assert arrow.equals(source)
    dataset = ray_runtime.data.from_arrow(arrow)
    result = dataset.map_batches(
        lambda batch: batch.append_column(
            "doubled_id", pa.compute.multiply(batch["id"], 2)
        ),
        batch_format="pyarrow",
    )
    assert sorted(result.take_all(), key=lambda row: row["id"]) == [
        {**row, "doubled_id": row["id"] * 2} for row in source.to_pylist()
    ]
