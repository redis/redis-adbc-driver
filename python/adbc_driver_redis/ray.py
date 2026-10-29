"""Read-only Ray Data ingestion using Redis ADBC partition descriptors."""

from functools import partial

import pyarrow as pa
import ray.data
from ray.data.block import BlockMetadata
from ray.data.datasource import Datasource, ReadTask

from . import dbapi


def _positive_int(name, value, maximum=None):
    if isinstance(value, bool) or not isinstance(value, int) or value < 1:
        raise ValueError(f"{name} must be a positive integer")
    if maximum is not None and value > maximum:
        raise ValueError(f"{name} must be at most {maximum}")


def _read_partition(descriptor, *, uri, schema, db_kwargs, conn_kwargs):
    try:
        yield from _stream_partition(
            descriptor, uri=uri, schema=schema,
            db_kwargs=db_kwargs, conn_kwargs=conn_kwargs,
        )
    except (dbapi.Error, OSError) as exc:
        # ADBC exception constructors require keyword-only status arguments and
        # cannot currently round-trip through Ray's exception serializer.
        raise RuntimeError(f"Redis partition read failed: {exc}") from None


def _stream_partition(descriptor, *, uri, schema, db_kwargs, conn_kwargs):
    # Only configuration, schema and opaque bytes cross the process boundary.
    # The generator owns all handles until exhausted, closed or unwound.
    with dbapi.connect(uri, db_kwargs=db_kwargs, conn_kwargs=conn_kwargs) as conn:
        with conn.cursor() as cur:
            cur.adbc_read_partition(descriptor)
            with cur.fetch_record_batch() as reader:
                if not reader.schema.equals(schema, check_metadata=True):
                    raise ValueError("Redis partition schema differs from planned schema")
                emitted = False
                for batch in reader:
                    emitted = True
                    yield pa.Table.from_batches([batch], schema=schema)
                if not emitted:
                    yield schema.empty_table()


class RedisDatasource(Datasource):
    """A planned live Redis read; construct with :func:`read_query`.

    Planning opens and closes a connection immediately. Rows are fetched only
    by Ray workers. Descriptors may be replayed, but live data can change.
    """

    def __init__(
        self, uri, query, *, parameters=None, partition_count=8,
        batch_rows=65536, partition_scan_cap=0, db_kwargs=None, conn_kwargs=None,
    ):
        super().__init__()
        if not isinstance(query, str) or not query.strip():
            raise ValueError("query must be a nonempty SQL string")
        _positive_int("partition_count", partition_count, 1024)
        _positive_int("batch_rows", batch_rows)
        if (isinstance(partition_scan_cap, bool)
                or not isinstance(partition_scan_cap, int)
                or not 0 <= partition_scan_cap <= 2147483648):
            raise ValueError("partition_scan_cap must be an integer in 0..2147483648")
        self._uri = uri
        self._db_kwargs = dict(db_kwargs or {})
        self._db_kwargs["adbc.redis.stream_batch_rows"] = str(batch_rows)
        self._conn_kwargs = dict(conn_kwargs or {})
        with dbapi.connect(
            uri, db_kwargs=self._db_kwargs, conn_kwargs=self._conn_kwargs
        ) as conn, conn.cursor() as cur:
            cur.adbc_statement.set_options(**{
                "adbc.redis.partition_count": str(partition_count),
                "adbc.redis.partition_scan_cap": str(partition_scan_cap),
                "adbc.redis.partition_result_mode": "live",
            })
            partitions, self._schema = cur.adbc_execute_partitions(query, parameters)
            self._partitions = tuple(bytes(p) for p in partitions)

    def estimate_inmemory_data_size(self):
        return None

    def get_read_tasks(self, parallelism, per_task_row_limit=None, data_context=None):
        # The driver's complete, nonoverlapping split set controls task count.
        # Ray's parallelism and row-limit hints never change query membership.
        return [
            ReadTask(
                partial(
                    _read_partition, descriptor, uri=self._uri,
                    schema=self._schema, db_kwargs=self._db_kwargs,
                    conn_kwargs=self._conn_kwargs,
                ),
                BlockMetadata(num_rows=None, size_bytes=None, exec_stats=None),
                schema=self._schema,
            )
            for descriptor in self._partitions
        ]


def read_query(
    uri, query, *, parameters=None, partition_count=8, batch_rows=65536,
    concurrency=None, partition_scan_cap=0, db_kwargs=None, conn_kwargs=None,
):
    """Plan a supported SELECT and stream its partitions on Ray workers.

    Requires the ``ray`` extra and a partition-capable native driver on the
    caller and every worker. Query planning is eager; row consumption is lazy.
    ``partition_count`` (1..1024) requests splits, not equally sized results;
    ``concurrency`` caps simultaneous Ray read tasks. ``batch_rows`` bounds each
    driver Arrow batch, not total Ray object-store or pipeline memory.

    Reads are unordered and live: no shared snapshot, stable retries, CDC or
    exactly-once guarantee. Unsupported SQL fails during planning, without a
    materializing fallback. Worker-local TLS CA paths must exist on all nodes.
    Configuration may contain credentials; use a trusted Ray cluster.
    """
    if concurrency is not None:
        _positive_int("concurrency", concurrency)
    source = RedisDatasource(
        uri, query, parameters=parameters, partition_count=partition_count,
        batch_rows=batch_rows, partition_scan_cap=partition_scan_cap,
        db_kwargs=db_kwargs, conn_kwargs=conn_kwargs,
    )
    return ray.data.read_datasource(source, concurrency=concurrency)
