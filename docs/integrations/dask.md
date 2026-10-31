# Redis integration guide for Dask

Redis must run separately with the Query Engine (Search). See
[installation and driver discovery](../python-integrations.md#install-and-discover-the-driver)
for Python/native setup, supported wheel platforms, TLS and source builds.
Tables must be created, ingested or adopted through the driver; arbitrary Redis
keys do not automatically become SQL tables. Writes autocommit without rollback.

This guide describes current repository capabilities. A newly added helper or
extra may require a build containing its implementation. If a released wheel
lacks it, build the native driver from this checkout and install the relevant
extra from `./python`; set `REDIS_ADBC_LIBRARY` to the built library. Do not infer
that an older published wheel contains every helper documented here.

## Dask

Install `adbc-driver-redis[dask]` in the client and every worker environment. This
extra includes Dask DataFrame, Distributed, pandas and PyArrow. Platform wheels
bundle the native driver; source installs require `REDIS_ADBC_LIBRARY` set to a
local library path **on every worker before starting it**. Workers need network
access to the Redis endpoint; `localhost` refers to each worker's machine. Keep
package versions consistent across client, scheduler and workers. Use `rediss://`
and `db_kwargs`/`conn_kwargs` just as with `dbapi.connect`.

`adbc_driver_redis.dask.read_query` uses
[`dd.from_map`](https://docs.dask.org/en/stable/generated/dask.dataframe.from_map.html)
to run one bound SELECT for each supplied parameter tuple:

```python
import pyarrow as pa
from adbc_driver_redis.dask import read_query

# Existing table, with non-NULL ids in [0, 3000).
frame = read_query(
    "redis://localhost:6379/0",
    "SELECT id, amount FROM orders WHERE id >= ? AND id < ?",
    [(0, 1000), (1000, 2000), (2000, 3000)],
    schema=pa.schema([("id", pa.int64()), ("amount", pa.float64())]),
    db_kwargs={"adbc.redis.query_workers": "1"},
)
print(frame.groupby("id").amount.sum().compute())
```

The driver supports these bound half-open range predicates. This helper does
not rely on ADBC partition descriptors, SQLAlchemy, OFFSET pagination, automatic
MIN/MAX probes, or fetching sample rows. The supplied Arrow schema describes
the SELECT result exactly (column names, order, types and nullability; schema
metadata is ignored). A mismatch fails the task, rather than coercing data.
The result uses pandas Arrow dtypes, so nullable integers and empty partitions
retain their types. Pass `[()]` for one unparameterized query. Parameters are
bound through DB-API; identifiers belong in your SQL, not parameter values.

Choose disjoint predicates that cover your intended dataset. Numeric ranges
exclude NULLs; add a separate NULL query partition when needed, as shown in
the runnable example. Rows outside your bounds are excluded. Dask index
divisions are unknown, indexes restart per partition, and row order is not
guaranteed. There is no automatic inference or validation of partition coverage.
Redis provides no shared snapshot across tasks: concurrent updates, key changes,
and retried or repeated computations can yield different, missing, or duplicate
rows. Read an immutable source or pause writers when exact coverage is required.

Each task opens and closes a connection and cursor on its worker, including on
execution/fetch failures. Only query configuration, schema and parameters are
sent to workers. Connection credentials are part of the task configuration;
use a trusted Dask cluster and protect its scheduler and diagnostics.

A whole query partition is fetched into an Arrow table and converted to pandas
in worker memory. Choose bounds using your data distribution, allow space for
Arrow and pandas plus concurrent tasks, and split skewed ranges further.
Streaming batch options do not cap this materialization. Limit active tasks and
`adbc.redis.query_workers` to avoid multiplying driver concurrency across worker
processes. This adapter does not push later Dask filters/projections into SQL;
select only the required columns and predicates in the query. For large results,
write partitions to Parquet or compute reductions instead of gathering the full
DataFrame into the client.

Run the self-contained example against a disposable Redis with Search:

```sh
python -m pip install -e './python[dask]'
export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" # .so on Linux
REDIS_URI=redis://localhost:6379/0 python python/examples/dask_read.py
```

The example creates a unique table, starts two Distributed worker processes,
checks five partitions (including an empty range and a NULL partition), compares
all rows and dtypes with the source, and drops its table. Its `__main__` guard is
required when spawning worker processes. To use an existing cluster, replace
`LocalCluster(...)` with `Client("tcp://scheduler:8786")`.

Dask's built-in `read_sql_table`/`read_sql_query` APIs require SQLAlchemy URIs,
not direct ADBC connections. This package also provides a SQLAlchemy dialect
(`redis+adbc://`); that is a separate route with Dask's SQLAlchemy query and
partitioning requirements. The helper here directly uses ADBC with explicit
partitions and schema.

## Rerun this guide

From the repository root, with the guide's dependencies and native driver
installed, set `REDIS_URI` to the dedicated local Software fixture and run:

```sh
python python/scripts/check_integration_guide.py dask
```

This executes the guide's code blocks or its linked runnable example. For
Superset it exercises the documented backend workflow in an isolated application;
it does not automate the browser UI. See the
[per-guide results](../sdlc/changes/integration-guides-262/validation.md).

[All integration guides](README.md)
