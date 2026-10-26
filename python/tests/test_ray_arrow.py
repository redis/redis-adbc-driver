"""The small-data Redis ADBC -> Arrow table -> Ray Data bridge."""

import datetime as dt
from decimal import Decimal
import uuid

import pyarrow as pa
import pytest

ray = pytest.importorskip("ray")

from adbc_driver_redis import dbapi


@pytest.fixture(scope="module")
def ray_runtime():
    ray.init(
        num_cpus=2,
        include_dashboard=False,
        log_to_driver=False,
        object_store_memory=128 * 1024 * 1024,
    )
    try:
        yield
    finally:
        ray.shutdown()


def assert_ray_roundtrip(table):
    dataset = ray.data.from_arrow(table)
    assert dataset.count() == table.num_rows
    blocks = ray.get(dataset.to_arrow_refs())
    assert blocks, "Ray must retain the Arrow schema even for an empty table"
    actual = pa.concat_tables(blocks)
    assert actual.schema.equals(table.schema, check_metadata=True)
    assert actual.equals(table, check_metadata=True)
    return dataset


def test_ray_from_arrow_select_one(uri, ray_runtime):
    # This is the exact composition shown in the upstream Ray proposal.
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute("SELECT 1 AS one")
        table = cur.fetch_arrow_table()
    dataset = assert_ray_roundtrip(table)
    assert dataset.take_all() == [{"one": 1}]


@pytest.mark.parametrize("empty", [False, True])
def test_ray_from_arrow_types_and_nulls(uri, ray_runtime, empty):
    table_name = f"it_python_ray_arrow_{uuid.uuid4().hex}"
    source = pa.table(
        {
            "id": pa.array([1, 2, 3, 4], type=pa.int64()),
            "label": pa.array(["hello", None, "Redis → Ray", ""], type=pa.string()),
            "amount": pa.array(
                [Decimal("1.23"), None, Decimal("-9.87"), Decimal("0.00")],
                type=pa.decimal128(12, 2),
            ),
            "flag": pa.array([True, False, None, True], type=pa.bool_()),
            "day": pa.array(
                [dt.date(2024, 1, 1), None, dt.date(2024, 2, 29), dt.date(2024, 12, 31)],
                type=pa.date32(),
            ),
            "instant": pa.array(
                [
                    dt.datetime(2024, 1, 1, 1, 2, 3, 456789),
                    None,
                    dt.datetime(2024, 2, 29),
                    dt.datetime(2024, 12, 31),
                ],
                type=pa.timestamp("us"),
            ),
            "payload": pa.array([b"\x00\xff", None, b"", b"redis"], type=pa.binary()),
        }
    )
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.adbc_ingest(table_name, source, mode="create")
    try:
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            where = " WHERE false" if empty else ""
            cur.execute(f'SELECT * FROM "{table_name}"{where} ORDER BY __rowid')
            table = cur.fetch_arrow_table()
        assert table.to_pylist() == ([] if empty else source.to_pylist())
        dataset = assert_ray_roundtrip(table)
        if not empty:
            # Also force a normal Ray Data operation in a worker process.
            def identity(batch):
                return batch

            processed = dataset.map_batches(
                identity, batch_format="pyarrow"
            ).materialize()
            actual = pa.concat_tables(ray.get(processed.to_arrow_refs()))
            assert actual.equals(table, check_metadata=True)
    finally:
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            cur.execute(f'DROP TABLE IF EXISTS "{table_name}"')
