"""Run against a disposable Redis with Search: python python/examples/dask_read.py."""

import os
import uuid

import pandas as pd
import pyarrow as pa
from adbc_driver_redis import dbapi
from adbc_driver_redis.dask import read_query
from distributed import Client, LocalCluster


def main():
    uri = os.environ.get("REDIS_URI", "redis://localhost:6379/0")
    table = f"example_dask_{uuid.uuid4().hex}"
    schema = pa.schema([("id", pa.int64()), ("name", pa.string())])
    source = pa.table(
        {"id": [-1, 0, 1, 2, 3, None], "name": ["a", "b", None, "d", "e", "f"]},
        schema=schema,
    )
    # A bound boolean selects the NULL partition without interpolating values.
    query = f'SELECT id, name FROM "{table}" WHERE (id >= ? AND id < ?) OR (id IS NULL AND ?)'
    partitions = [
        (-2, 0, False),
        (0, 2, False),
        (2, 4, False),
        (4, 6, False),
        (0, 0, True),
    ]
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.adbc_ingest(table, source, mode="create")
        try:
            frame = read_query(uri, query, partitions, schema=schema)
            with (
                LocalCluster(
                    n_workers=2,
                    threads_per_worker=1,
                    processes=True,
                    dashboard_address=None,
                ) as cluster,
                Client(cluster),
            ):
                sizes = frame.map_partitions(len).compute().tolist()
                result = (
                    frame.compute()
                    .sort_values("id", na_position="last")
                    .reset_index(drop=True)
                )
            expected = source.to_pandas(types_mapper=pd.ArrowDtype)
            pd.testing.assert_frame_equal(result, expected)
            assert sizes == [1, 2, 2, 0, 1], sizes
            print(f"Partition sizes: {sizes}")
            print(result)
        finally:
            cur.execute(f'DROP TABLE "{table}"')


if __name__ == "__main__":
    main()
