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


def write_result(table):
    """Write a tool's Arrow output and verify it through a fresh connection.

    This local example sink fully materializes the output. Each invocation owns
    its table and cleans it up, including after a failed ingest or read. It is
    not a transactional, distributed, or retry-safe production sink.
    """
    if not isinstance(table, pa.Table) or not table.num_columns:
        raise ValueError("expected an Arrow table with at least one column")
    if "guide_row_order" in table.column_names:
        raise ValueError("guide_row_order is reserved for example row ordering")
    name = "guide_output_" + uuid4().hex
    columns = ", ".join('"' + column.replace('"', '""') + '"' for column in table.column_names)
    ordered = table.append_column("guide_row_order", pa.array(range(table.num_rows), type=pa.int64()))
    with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
        try:
            count = cur.adbc_ingest(name, ordered, mode="create")
            assert count == table.num_rows, (count, table.num_rows)
            with dbapi.connect(os.environ["REDIS_URI"]) as reader, reader.cursor() as read:
                read.execute(f'SELECT {columns} FROM "{name}" ORDER BY guide_row_order')
                result = read.fetch_arrow_table()
                assert result.equals(table, check_metadata=False), (result, table)
            print(f"REDIS_ADBC_READ_WRITE_OK rows={table.num_rows}", flush=True)
            return result
        finally:
            cur.execute(f'DROP TABLE IF EXISTS "{name}"')
