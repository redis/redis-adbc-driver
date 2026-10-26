"""Read an existing managed table across processes using ADBC partition IDs.

python adbc_partitions.py 'SELECT id FROM public.events' --partitions 4
Set REDIS_URI to a worker-accessible Redis URI. Requires a driver build with
partition API support, adbc-driver-redis[dbapi], and PyArrow on each worker.
"""

import argparse
from concurrent.futures import ProcessPoolExecutor
import os

from adbc_driver_redis import dbapi


def count_partition(descriptor):
    with dbapi.connect(os.environ.get("REDIS_URI", "redis://localhost:6379/0")) as conn:
        with conn.cursor() as cur:
            cur.adbc_read_partition(descriptor)
            with cur.fetch_record_batch() as reader:
                return reader.schema, sum(batch.num_rows for batch in reader)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("sql")
    parser.add_argument("--partitions", type=int, default=4)
    parser.add_argument("--workers", type=int, default=4)
    args = parser.parse_args()
    with dbapi.connect(os.environ.get("REDIS_URI", "redis://localhost:6379/0")) as conn:
        with conn.cursor() as cur:
            cur.adbc_statement.set_options(**{"adbc.redis.partition_count": args.partitions})
            partitions, schema = cur.adbc_execute_partitions(args.sql)
    total = 0
    with ProcessPoolExecutor(max_workers=args.workers) as pool:
        for actual_schema, rows in pool.map(count_partition, partitions):
            if not actual_schema.equals(schema):
                raise RuntimeError("partition schema differs from planned schema")
            total += rows
    print(f"Read {total} rows from {len(partitions)} partitions")


if __name__ == "__main__":
    main()
