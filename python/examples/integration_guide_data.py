"""Small, owned Redis tables for the integration guide examples."""
from contextlib import contextmanager
import os
import re
from uuid import uuid4

import pyarrow as pa
from adbc_driver_redis import dbapi


@contextmanager
def example_table():
    name = "guide_" + uuid4().hex
    rows = pa.table({
        "id": pa.array([1, 2, 3], type=pa.int64()),
        "label": pa.array(["alpha", None, "雪"], type=pa.string()),
        "amount": pa.array([2.5, None, 7.5], type=pa.float64()),
        "active": pa.array([True, None, False], type=pa.bool_()),
    })
    with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
        try:
            cur.adbc_ingest(name, rows, mode="create")
            yield name
        finally:
            cur.execute(f'DROP TABLE IF EXISTS "{name}"')


def read_rows(name, minimum=1):
    if not re.fullmatch(r"guide_[0-9a-f]{32}", name):
        raise ValueError("expected an example-owned table name")
    with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
        cur.execute(f'SELECT * FROM "{name}" WHERE id >= ? ORDER BY id', (minimum,))
        return cur.fetch_arrow_table()
