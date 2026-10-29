"""Append-only dlt destination using Arrow ingestion through Redis ADBC."""

from typing import Dict, Optional

import dlt
import pyarrow as pa
from dlt.common.destination.exceptions import DestinationTerminalException
from dlt.common.schema.typing import TTableSchema

from . import dbapi


def _quote(name: str) -> str:
    if not isinstance(name, str) or not name or "\x00" in name:
        raise DestinationTerminalException(
            "SQL names must be nonempty strings without NUL"
        )
    return '"' + name.replace('"', '""') + '"'


@dlt.destination(
    name="redis_adbc",
    batch_size=1000,
    loader_file_format="parquet",
    naming_convention="snake_case",
    skip_dlt_columns_and_tables=True,
    loader_parallelism_strategy="sequential",
)
def redis_adbc(
    items: pa.RecordBatch,
    table: TTableSchema,
    uri: str = dlt.secrets.value,
    schema_name: str = "public",
    db_kwargs: Optional[Dict[str, str]] = None,
    index_columns: str = "*",
) -> None:
    """Load dlt resources into ``schema_name`` with ADBC create/append ingestion.

    Install ``adbc-driver-redis[dlt]``. Pass ``uri`` explicitly or configure
    ``destination.redis_adbc.uri`` as a dlt secret. ``schema_name`` is the Redis
    SQL schema, independent of dlt's dataset_name. Existing tables must accept
    the incoming columns and types; migrations are managed by the caller.

    Only append is supported. No primary-key upsert, rollback or exactly-once
    delivery is provided. Keep dlt's local pipeline state for checkpoints and
    incremental sources. A failed batch may partially apply; retrying it can
    duplicate rows. Serialize independent pipelines writing to the same table.
    """
    if table.get("write_disposition", "append") != "append":
        raise DestinationTerminalException(
            "Redis ADBC dlt destination supports write_disposition='append' only"
        )
    schema = _quote(schema_name)
    _quote(table["name"])
    if not isinstance(items, pa.RecordBatch):
        raise DestinationTerminalException(
            "Redis ADBC dlt destination requires Parquet Arrow batches; "
            "keep loader_file_format='parquet' and batch_size > 0"
        )
    if items.num_rows == 0:
        return
    data = pa.Table.from_batches([items])
    with dbapi.connect(uri, db_kwargs=db_kwargs) as conn, conn.cursor() as cur:
        cur.execute(f"CREATE SCHEMA IF NOT EXISTS {schema}")
        cur.adbc_statement.set_options(
            **{"adbc.redis.ingest.index_columns": index_columns}
        )
        cur.adbc_ingest(
            table["name"], data, mode="create_append", db_schema_name=schema_name
        )
