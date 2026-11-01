"""Materialize dependent assets through Redis, verify the SQL result, and clean up."""

import os
import uuid

import pyarrow as pa
from dagster import asset, materialize

from adbc_driver_redis import dbapi
from adbc_driver_redis.dagster import RedisADBCIOManager


@asset(io_manager_key="redis")
def orders() -> pa.Table:
    return pa.table({"id": [1, 2, 3], "amount": [10, 20, 12]})


@asset(io_manager_key="redis")
def revenue(orders: pa.Table) -> pa.Table:
    # Dagster loads orders from Redis through the IO manager.
    return pa.table(
        {"order_count": [orders.num_rows], "total": [sum(orders["amount"].to_pylist())]}
    )


def main():
    uri = os.environ["REDIS_URI"]
    schema = f"dagster_smoke_{uuid.uuid4().hex}"
    manager = RedisADBCIOManager(uri=uri, schema_name=schema)
    try:
        result = materialize([orders, revenue], resources={"redis": manager})
        if not result.success:
            raise RuntimeError("Dagster materialization failed")
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            cur.execute(f'SELECT order_count, total FROM "{schema}".revenue')
            rows = cur.fetchall()
        if rows != [(3, 42)]:
            raise AssertionError(f"Unexpected revenue persisted in Redis: {rows!r}")
        print("REDIS_ADBC_READ_WRITE_OK")
        print("Dagster → Redis → Dagster → Redis: 3 orders, total 42; SQL verification passed.")
    finally:
        # The generated schema belongs exclusively to this invocation.
        with dbapi.connect(uri) as conn, conn.cursor() as cur:
            cur.execute(f'DROP SCHEMA IF EXISTS "{schema}" CASCADE')


if __name__ == "__main__":
    main()
