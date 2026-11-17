"""Partitioned Dask reads and append writes over worker-local ADBC connections."""

from collections.abc import Sequence

import dask.dataframe as dd
from dask import annotate, delayed
import pandas as pd
import pyarrow as pa

from . import dbapi


class PartitionWriteError(RuntimeError):
    """A failed append and the preceding tasks' acknowledged row count.

    The failing partition can have committed rows when ``write_attempted`` is
    true. Counts do not include writes from recomputed or repeated tasks.
    """

    def __init__(self, partition_index, acknowledged_rows, write_attempted, message):
        # Preserve constructor arguments so Distributed can serialize the error.
        super().__init__(partition_index, acknowledged_rows, write_attempted, message)
        self.partition_index = partition_index
        self.acknowledged_rows = acknowledged_rows
        self.write_attempted = write_attempted
        self.message = message

    def __str__(self):
        return (
            f"Redis append partition {self.partition_index} failed: {self.message}; "
            f"preceding partitions acknowledged {self.acknowledged_rows} rows; "
            f"current partition write attempted: {self.write_attempted}"
        )


def _append_partition(
    partition, acknowledged_rows, *, partition_index, uri, table, schema,
    db_schema_name, db_kwargs, conn_kwargs,
):
    attempted = False
    try:
        if list(partition.columns) != schema.names:
            raise ValueError("partition columns must exactly match the Arrow schema")
        data = pa.Table.from_pandas(partition, schema=schema, preserve_index=False, safe=True)
        with (
            dbapi.connect(uri, db_kwargs=db_kwargs, conn_kwargs=conn_kwargs) as conn,
            conn.cursor() as cur,
        ):
            attempted = True
            count = cur.adbc_ingest(
                table, data, mode="append", db_schema_name=db_schema_name
            )
        if count != data.num_rows:
            raise RuntimeError("ingestion did not acknowledge the complete partition")
        return acknowledged_rows + count
    except Exception as exc:
        raise PartitionWriteError(
            partition_index, acknowledged_rows, attempted,
            "append or cleanup failed" if attempted else "conversion or connection failed",
        ) from exc


def write_dataframe(
    frame, uri, table, *, schema, db_schema_name=None, allow_duplicate_writes=False,
    db_kwargs=None, conn_kwargs=None,
):
    """Build a lazy worker-local Arrow append into an existing table.

    Each partition opens and closes its own connection/cursor. Appends form a
    sequential dependency chain; each write converts one partition. Dask controls
    upstream scheduling and spilling and may materialize many partitions at once.
    No complete frame is gathered into the caller. The supplied Arrow schema controls
    safe conversion, ignores the pandas index and must match column names/order.
    The target must already exist and accept that schema. No target is created,
    replaced or dropped. Serialize other writers to this target yourself.

    ``allow_duplicate_writes=True`` is required: Dask can recompute a task after
    worker loss even with zero retries. Recomputing this graph or retrying a
    failed partition can append duplicates. There is no rollback, resume or
    exactly-once guarantee. Ordinary task-error retries are disabled by default;
    scheduler overrides and worker-loss recomputation remain possible.

    Compute the returned Delayed once to obtain the acknowledged row count for
    successful tasks in that evaluation, excluding any replayed attempts.
    PartitionWriteError retains the failing partition index and preceding task
    count; the failing partition may also have committed rows. Inspect the table
    after failure. The helper never retries or deletes committed data.
    """
    if allow_duplicate_writes is not True:
        raise ValueError("allow_duplicate_writes=True is required to acknowledge Dask replay")
    if not isinstance(frame, dd.DataFrame):
        raise TypeError("frame must be a Dask DataFrame")
    if not isinstance(schema, pa.Schema):
        raise TypeError("schema must be a pyarrow.Schema")
    if not schema.names or len(set(schema.names)) != len(schema.names):
        raise ValueError("schema must have nonempty, unique column names")
    if any(not name or "\x00" in name for name in schema.names):
        raise ValueError("schema column names must be nonempty and contain no NUL")
    if list(frame.columns) != schema.names:
        raise ValueError("frame columns must exactly match the Arrow schema")
    for name in (table, db_schema_name):
        if name is not None and (not isinstance(name, str) or not name or "\x00" in name):
            raise ValueError("table and schema names must be nonempty strings without NUL")
    if table is None:
        raise ValueError("table must be a nonempty string")
    partitions = frame.to_delayed()
    count = 0
    with annotate(retries=0):
        for index, partition in enumerate(partitions):
            count = delayed(_append_partition, pure=False)(
                partition, count, partition_index=index, uri=uri, table=table,
                schema=schema, db_schema_name=db_schema_name,
                db_kwargs=dict(db_kwargs or {}), conn_kwargs=dict(conn_kwargs or {}),
            )
    return count


def _fetch_partition(parameters, *, uri, query, schema, db_kwargs, conn_kwargs):
    with (
        dbapi.connect(uri, db_kwargs=db_kwargs, conn_kwargs=conn_kwargs) as conn,
        conn.cursor() as cur,
    ):
        cur.execute(query, parameters or None)
        table = cur.fetch_arrow_table()
    if not table.schema.equals(schema, check_metadata=False):
        raise ValueError(
            f"Redis query partition schema differs from schema: "
            f"expected {schema}, received {table.schema}"
        )
    return table.to_pandas(types_mapper=pd.ArrowDtype)


def read_query(uri, query, partitions, *, schema, db_kwargs=None, conn_kwargs=None):
    """Build a lazy DataFrame from one parameterized SELECT per partition.

    ``partitions`` is a nonempty iterable of positional parameter sequences for
    the query's ``?`` placeholders. The caller must provide disjoint, exhaustive
    predicates, including a separate NULL partition when needed. For a single
    unparameterized query use ``[()]``. ``schema`` is the exact result Arrow
    schema; no database connection is opened to infer metadata. Every partition
    must return that schema, including empty partitions.

    Each task owns and closes its connection and cursor. Results use pandas
    Arrow dtypes and unknown Dask index divisions. A partition is materialized
    in worker memory; reads and retries have no cross-partition snapshot.
    Install the package's ``dask`` extra and native driver on every worker.
    """
    if not isinstance(query, str) or not query.strip():
        raise ValueError("query must be a nonempty SQL string")
    if not isinstance(schema, pa.Schema):
        raise TypeError("schema must be a pyarrow.Schema")
    parameters = []
    for partition in partitions:
        if not isinstance(partition, Sequence) or isinstance(partition, (str, bytes)):
            raise TypeError("each partition must be a positional parameter sequence")
        parameters.append(tuple(partition))
    if not parameters:
        raise ValueError("partitions must contain at least one parameter sequence")
    meta = schema.empty_table().to_pandas(types_mapper=pd.ArrowDtype)
    return dd.from_map(
        _fetch_partition,
        parameters,
        uri=uri,
        query=query,
        schema=schema,
        db_kwargs=dict(db_kwargs or {}),
        conn_kwargs=dict(conn_kwargs or {}),
        meta=meta,
        enforce_metadata=True,
    )
