"""Byte-budget behavior and exact output through the native ADBC boundary."""
import os

import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
import pytest


@pytest.mark.parametrize("api", ["regular", "partition"])
@pytest.mark.parametrize("expression", [False, True])
def test_stream_byte_budget(driver_path, db_kwargs, api, expression):
    table = "it_native_byte_budget"
    data = pa.table({"id": pa.array([9007199254740993 + i for i in range(1000)], pa.int64()),
                     "label": [None if i % 7 == 0 else "large\x00" * (4000 if i == 3 else i % 9) for i in range(1000)],
                     "payload": pa.array([b"\x00\xff"] * 1000, pa.binary())})
    options = {**db_kwargs, "adbc.redis.stream_batch_rows": "7",
               "adbc.redis.stream_batch_bytes": "1024"}
    label_sql = "label || '!'" if expression else "label"
    sql = f"SELECT id, {label_sql} AS label, payload FROM {table}"
    with dbapi.connect(driver=driver_path, db_kwargs=db_kwargs, autocommit=True) as conn, conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        cur.adbc_ingest(table, data)
    try:
        descriptors = [None]
        if api == "partition":
            with dbapi.connect(driver=driver_path, db_kwargs=options, autocommit=True) as conn, conn.cursor() as cur:
                cur.adbc_statement.set_options(**{"adbc.redis.partition_count": 4})
                descriptors, _ = cur.adbc_execute_partitions(sql)
        batches = []
        worker_options = dict(options)
        if api == "partition":
            worker_options["uri"] = os.environ.get("REDIS_WORKER_URI") or options["uri"]
        for descriptor in descriptors:
            with dbapi.connect(driver=driver_path, db_kwargs=worker_options, autocommit=True) as conn, conn.cursor() as cur:
                if descriptor is None:
                    cur.adbc_statement.set_options(**{"adbc.redis.stream_batch_bytes": 1024})
                    assert cur.adbc_statement.get_option_int("adbc.redis.stream_batch_bytes") == 1024
                    cur.execute(sql)
                else:
                    cur.adbc_read_partition(descriptor)
                with cur.fetch_record_batch() as reader:
                    for batch in reader:
                        assert 0 < batch.num_rows <= 7
                        cost = sum(768 + 2 * len((row['label'] or '').encode()) +
                                   2 * len(row['payload'] or b'') for row in batch.to_pylist())
                        assert batch.num_rows == 1 or cost <= 1024
                        batches.append(batch)
        actual = pa.Table.from_batches(batches).sort_by([("id", "ascending")])
        if expression:
            data = data.set_column(1, 'label', pa.array([None if v is None else v + '!' for v in data['label'].to_pylist()]))
        assert actual.equals(data, check_metadata=False)
    finally:
        with dbapi.connect(driver=driver_path, db_kwargs=db_kwargs, autocommit=True) as conn, conn.cursor() as cur:
            cur.execute(f"DROP TABLE IF EXISTS {table}")
