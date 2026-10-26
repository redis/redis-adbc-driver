"""Partitioned Dask reads over worker-local ADBC connections."""

from collections.abc import Sequence

import dask.dataframe as dd
import pandas as pd
import pyarrow as pa

from . import dbapi


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
