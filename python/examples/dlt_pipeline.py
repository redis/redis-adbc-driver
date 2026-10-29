"""Run with REDIS_URI pointing to Redis with Search; appends on each run."""

import os

import dlt
from adbc_driver_redis import dbapi
from adbc_driver_redis.dlt import redis_adbc


def main():
    uri = os.environ["REDIS_URI"]
    pipeline = dlt.pipeline(
        pipeline_name="redis_orders",
        destination=redis_adbc(uri=uri, schema_name="dlt_example"),
    )
    info = pipeline.run(
        [{"id": 1, "amount": 10.5}, {"id": 2, "amount": 20.0}],
        table_name="orders",
        write_disposition="append",
    )
    info.raise_on_failed_jobs()
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute("SELECT id, amount FROM dlt_example.orders ORDER BY id")
        print(cur.fetch_arrow_table())


if __name__ == "__main__":
    main()
