# Python analytics integrations

The standard way to integrate your application with Redis is to use the
[official Redis clients and libraries](https://redis.io/docs/latest/develop/clients/)
for your programming language. Redis ADBC is another option for workflows that
benefit from SQL and Apache Arrow, including the data-tool integrations described here.

For a tool-by-tool starting point and the `doctor` command, see
[Choose your tool and connect](setup.md).
For native clients outside Python, the [managed installer](native-installation.md)
downloads and registers the library and supports upgrade/uninstall.

These integrations use the Redis ADBC library, not Redis's wire protocol as a
PostgreSQL server. Search is required. Writes autocommit; transaction scopes do
not provide rollback or isolation. Use a dedicated Redis database for tests.

See the [dedicated Redis integration guides](integrations/README.md) for individual
tools and the upstream documentation tracking issue. The
[read/write guide harness](integrations/example-setup.md#require-both-directions-in-the-harness)
verifies computed-output writes and independent read-back across 36 native or
custom application paths; [dated evidence](sdlc/changes/integration-read-write/validation.md)
records the tested versions and remaining gaps.

**Recommended Redis configuration:** Redis Search (Query Engine) enabled
and query performance factor (QPF) set to **4×** where supported.
Search is required; 4× QPF is recommended for concurrent query performance
and needs sufficient server CPU capacity. Configure these on the Redis server;
see [server requirements](driver-reference.md#server-requirements) and the
[QPF benchmarks](driver-reference.md#query-performance-factor-qpf).

## Install and discover the driver

Install from [PyPI](https://pypi.org/project/adbc-driver-redis/) (Python 3.14 recommended;
Linux amd64/arm64 or macOS arm64):

```sh
python -m pip install 'adbc-driver-redis[dbapi]' polars
```

Pip selects the platform wheel, which bundles the native driver. Redis with
Search must be running separately. Windows, macOS Intel and musl-based Linux
wheels are not provided. You can also install a wheel attached to a release:

```sh
python -m pip install '/path/to/adbc_driver_redis-<version>-py3-none-<platform>.whl[dbapi]'
```

The wheel bundles the native library. macOS release wheels target macOS 12+
(the Go 1.26 runtime minimum); the builder checks the library deployment target
before tagging. For development:

```sh
make -C go build
python -m pip install -e './python[test]'
export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so" # .dylib on macOS
```

```python
from adbc_driver_redis import dbapi, DatabaseOptions
import polars as pl

with dbapi.connect("redis://localhost:6379/0", db_kwargs={DatabaseOptions.TIME_ZONE.value: "UTC"}) as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT 1 AS one")
        print(cur.fetch_arrow_table())
print(pl.read_database_uri("SELECT 1 AS one", "redis://localhost:6379/0", engine="adbc"))
```

`adbc_driver_rediss.dbapi` is the TLS alias for URI-based clients. It preserves
TLS certificate verification. `autocommit=True` is the default; `False` raises
`NotSupportedError` immediately. The low-level `adbc_driver_redis.connect`
returns an owned `AdbcDatabase`, rather than a DB-API connection. Database and
statement option enums are available in `adbc_driver_redis`.

Non-Python tools need manifests even when the Python wheel is installed:

```sh
python -m adbc_driver_redis install-manifest --user
# Or activate a virtual environment, then use --venv.
# For a custom location: --directory /path/to/manifests
export ADBC_DRIVER_PATH=/path/to/manifests # only for a custom location
```

An unpacked native tarball includes `./install.sh [--user | --system | --venv]`
and supports `--directory`. Use Python 3.14; it needs no Python packages.
The installer writes both `redis.toml` and `rediss.toml`, following the
[ADBC manifest specification](https://arrow.apache.org/adbc/current/format/driver_manifests.html).
Paths are absolute: keep the library in place and rerun installation after moving
or replacing it. User installation defaults to `~/.config/adbc/drivers` on Linux
(`XDG_CONFIG_HOME` is honored) or `~/Library/Application Support/ADBC/Drivers`
on macOS. No privileged install is performed automatically.

With manifests installed, Python's manager also accepts
`dbapi.connect(uri="redis://localhost:6379/0", autocommit=True)` or
`driver="redis"`. Older managers may need a library path; the package requires
manager >=1.12.0.

## pandas SQL reads

Install `adbc-driver-redis[dbapi]` and pandas to pass an open ADBC
connection directly to `read_sql` or `read_sql_query`:

```python
import pandas as pd
from adbc_driver_redis import dbapi

with dbapi.connect("redis://localhost:6379/0") as conn:
    df = pd.read_sql_query("SELECT 42 AS answer", conn, dtype_backend="pyarrow")
    print(df)
```

Direct reads are covered by `python/tests/test_pandas_direct_reads.py`: default and Arrow
DataFrame types, SQL NULLs, Unicode, integers above 2^53, microsecond timestamps,
filtering, aggregation, index selection, empty results and multiple Arrow
batches. Local validation used pandas 3.1.0, PyArrow 26.0.0, ADBC manager 1.13.0
and Redis Software with Redis 8.6.2; see the
[validation record](sdlc/changes/pandas-direct-reads/validation.md).
The Arrow backend preserves nullable integer types. These tests cover SQL
queries; pandas table reads, parameters and `chunksize` are outside this
validation. For write coverage, see the next section.

## pandas writes and round trips

pandas 2.2+ accepts an open ADBC connection directly for
[`read_sql`](https://pandas.pydata.org/docs/reference/api/pandas.read_sql.html)
and [`DataFrame.to_sql`](https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.to_sql.html).
Install `adbc-driver-redis[dbapi]` and `pandas>=2.2`. This example creates a
unique table, verifies the fetched rows and removes the table afterward:

```python
import uuid
import pandas as pd
from adbc_driver_redis import dbapi

table = "pandas_demo_" + uuid.uuid4().hex
source = pd.DataFrame({"id": [1, 2, 3], "amount": [10.5, 20.0, 30.25]})

with dbapi.connect("redis://localhost:6379/0") as conn:
    try:
        source.to_sql(table, conn, index=False)
        result = pd.read_sql(f'SELECT * FROM "{table}" ORDER BY id', conn)
        pd.testing.assert_frame_equal(result, source)
        print(result)
    finally:
        with conn.cursor() as cur:
            cur.execute(f'DROP TABLE IF EXISTS "{table}"')
```

Use `if_exists="append"` to append rows. `if_exists="replace"` drops and
recreates the table; writes autocommit and cannot be rolled back. For nullable
Arrow dtypes on reads, pass `dtype_backend="pyarrow"`. No SQLAlchemy engine is
needed for this path.

Run the focused integration tests against a disposable Redis with Search:

```sh
REDIS_URI=redis://localhost:6379/0 python -m pytest -q python/tests/test_pandas_writes.py
```

The recorded run used pandas 3.1.0, PyArrow 26.0.0 and ADBC manager 1.13.0;
see [validation](sdlc/changes/pandas-smoke/validation.md).

## Prefect custom Python tasks

Install `pip install 'adbc-driver-redis[prefect]'`. Create connections inside each
task so concurrent tasks and retries each own their connection and cursor:

```python
from adbc_driver_redis import dbapi
from prefect import flow, task
from prefect.cache_policies import NO_CACHE

@task(cache_policy=NO_CACHE, persist_result=False)
def query(uri):
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute("SELECT 1 AS one")
        return cur.fetch_arrow_table()

@flow(persist_result=False)
def pipeline(uri):
    return query.submit(uri).result()

print(pipeline("redis://localhost:6379/0").to_pylist())
```

The [Prefect tests](../python/tests/test_prefect.py) run real local flows with
Arrow ingestion/fetch, task dependencies, concurrent queries, a retry after a
committed write, and SQL error propagation. Writes autocommit even if a task
fails. Make retried writes idempotent (the test replaces its output table);
blindly retrying append/INSERT can duplicate rows. Caching and result persistence
are disabled here so each invocation actually accesses Redis. Remote workers
need the driver wheel and Redis connectivity installed separately.

Run against a disposable Redis with Search:

```sh
python -m pip install -e './python[prefect]' pytest
REDIS_URI=redis://localhost:6379/0 \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
PREFECT_SERVER_ANALYTICS_ENABLED=false \
python -m pytest -q python/tests/test_prefect.py
```

Build the library first as above; use `.so` on Linux. This verifies custom Python
tasks, not a packaged Prefect ADBC connector, `prefect-redis`, Flight SQL, or a
remote Prefect deployment.

## SQLAlchemy and Superset

Install the wheel's `sqlalchemy` extra for SQLAlchemy >=2.1.6. Use the separate
`superset` extra in a Python 3.12 environment for Superset 6.1, which requires
SQLAlchemy 1.4 and older pandas/greenlet dependencies. The `superset` extra uses Arrow 18; the other extras use Arrow 26.
These extras cannot share an environment; Superset also cannot share the latest
pandas used by `dask` and `test`. The dialect supports both SQLAlchemy versions. It compiles PostgreSQL SQL and reflects
the driver's catalogs, including schemas, tables, views, column types, primary
keys, indexes and comments. Redis version metadata comes from ADBC; PostgreSQL
catalog compatibility is version 16, not the Redis server version.

```python
from sqlalchemy import create_engine, inspect, text

engine = create_engine("redis+adbc://localhost:6379/0")
print(inspect(engine).get_schema_names())
with engine.connect() as conn:
    print(conn.execute(text("SELECT :value AS value"), {"value": 42}).all())
engine.dispose()
```

Use `rediss+adbc://` for TLS. Connection options can be passed as
`create_engine(..., connect_args={"db_kwargs": {"adbc.redis.default_schema": "analytics"}})`.
URI query parameters follow the native driver's Redis URI options.

This is an analytics dialect. Only `AUTOCOMMIT` isolation is accepted. SQLAlchemy's
begin/commit/rollback housekeeping is a no-op; a failed context or `rollback()`
does **not** undo writes. Savepoints and two-phase transactions are rejected.
Use explicit values for IDs; ORM identity generation and transactional workloads
are outside this connector's contract.

In Superset, install the package in the same environment as every web worker and
SQL Lab worker, then restart them. The `superset.db_engine_specs` entry points
register Redis and Redis TLS; SQL parsing uses sqlglot's PostgreSQL dialect.
Add a database using `redis+adbc://host:6379/0`, test the connection, select a
schema and table, and create a dataset. `DATE_TRUNC` supplies PostgreSQL time
grains for time-series charts; grouped dimensions and `SUM` support bar charts.
Enable SQL Lab access as appropriate for that database. No PostgreSQL server or
psycopg dependency is needed. This does not add SQL constructs the driver lacks.

## Dagster

The community [`dagster-adbc`](https://pypi.org/project/dagster-adbc/) package
provides `ADBCResource` for SQL and Arrow access inside assets. Install
`dagster-adbc` alongside the Redis driver wheel. With the Redis driver manifest
installed as described above:

```python
from dagster import Definitions, asset
from dagster_adbc import ADBCResource

@asset
def order_count(redis: ADBCResource) -> int:
    with redis.get_connection() as conn, conn.cursor() as cur:
        cur.execute("SELECT count(*) FROM analytics.orders")
        return cur.fetchone()[0]

defs = Definitions(
    assets=[order_count],
    resources={"redis": ADBCResource(
        driver="redis",
        uri="redis://localhost:6379/0",
        autocommit=True,
    )},
)
```

Alternatively, set `driver` to the absolute native library path. Use
`autocommit=True`: Redis does not implement transactions, and the community
resource's default (`False`) produces a driver-manager warning. Native database
options can be passed through `db_kwargs`, for example
`{"adbc.redis.default_schema": "analytics"}`. The resource exposes a connection;
assets explicitly execute queries or call `cursor.adbc_ingest`.

Install the wheel's `dagster` extra, plus pandas/polars when used. This repository
ships `RedisADBCIOManager`; it is not part of the community `dagster-adbc` package.

```python
import pyarrow as pa
from dagster import Definitions, asset
from adbc_driver_redis.dagster import RedisADBCIOManager

@asset(io_manager_key="redis", key_prefix=["analytics"])
def orders() -> pa.Table:
    return pa.table({"id": [1, 2], "amount": [10, 20]})

defs = Definitions(
    assets=[orders],
    resources={"redis": RedisADBCIOManager(uri="redis://localhost:6379/0")},
)
```

For a runnable smoke test from a source checkout, use a dedicated Redis database
with Search and follow the development installation steps above. Run:

```sh
REDIS_URI=redis://localhost:6379/0 python python/examples/dagster_smoke.py
REDIS_URI=redis://localhost:6379/0 python -m pytest -q python/tests/test_dagster.py
```

The [smoke example](../python/examples/dagster_smoke.py) materializes `orders`,
loads it from Redis into the dependent `revenue` asset, and checks the persisted
result with SQL: three orders totaling 42. Each invocation uses a unique schema
and drops it on exit. The integration suite also checks Arrow/pandas/polars
round trips, column projection, partition retries, and failed-stream preservation;
it creates and removes the fixed `it_python_dagster` schema, so run it serially.

Outputs may be `pyarrow.Table`, `RecordBatchReader`, pandas or polars DataFrames.
Inputs default to Arrow tables; annotate them with pandas/polars DataFrame or
RecordBatchReader to request that type. Input `columns` metadata selects columns.
Reader outputs ingest in batches; reader inputs currently materialize the result
before returning an in-memory reader.

Names are lowercase; the final asset-key component is the table, preceding
components joined by underscores form the schema (`public` without a prefix).
`schema_name` overrides it. Configure `db_kwargs` for native options and
`index_columns` for ingestion indexing (`*` by default). Output metadata includes
row count, table name and Arrow column schema.

Unpartitioned writes load a unique staging table before dropping the old table
and renaming the stage. A failed ingest preserves the old version; a failure
between DROP and RENAME can leave it absent. Readers can also observe this brief
window. Static/dynamic string and daily/time-window partitions require asset
metadata `{"partition_expr": "column_name"}`. The manager uses bound partition
values or a half-open time range, deletes those rows, then appends. A retry
replaces that partition. A failed append may leave it partially written; values
returned by the asset must belong to the selected partition. Serialize writes to
the same asset/partition. Multi-dimensional partitions are unsupported.

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

## Ray Data

### Worker-side Arrow reader

The source-tree `ray` extra pins Ray 2.59.0. Install this build on the caller
and every worker (Python 3.12+), including its matching native library:

```sh
python -m pip install './python[ray]'
```

```python
import os
import ray
from adbc_driver_redis.ray import read_query

ray.init()
try:
    ds = read_query(
        os.environ["REDIS_URI"],
        "SELECT id, label FROM public.events WHERE id >= ?",
        parameters=(100,),
        partition_count=8,
        concurrency=2,
        batch_rows=65536,
    )
    print(ds.take(10))
finally:
    ray.shutdown()
```

`read_query` eagerly plans a supported SELECT through `ExecutePartitions` and
closes the planning handles. Ray read tasks open independent connections and
consume `ReadPartition` Arrow batches incrementally. Neither Python rows nor the
complete result are materialized on the caller. Empty reads retain the planned
Dataset schema; Ray may omit zero-row blocks from `to_arrow_refs()`.

The [live partition SQL subset](partitioned-reads.md#supported-queries) applies.
Unsupported queries fail at planning; there is no automatic materializing
fallback. The adapter explicitly selects live mode. Retained results, manual SQL
splits, writes and Flight SQL are outside this adapter's scope. Existing Ray SQL
read/write paths below are separate integrations.

`partition_count` requests 1..1024 splits; sparse or empty ranges can make them
uneven. `concurrency` caps simultaneous Ray read tasks independently of split
count. `batch_rows` controls native Arrow batch rows, not total pipeline memory;
Ray can combine or split blocks and buffer data in its object store. Output is
unordered. Reads and retries have no shared snapshot, stable replay or
exactly-once guarantee. Concurrent writes can change re-read values. Schema or
physical table changes can invalidate descriptors and require a new query plan.

`db_kwargs` and `conn_kwargs` are passed to caller and worker connections.
For private TLS CAs, use a verified `rediss://` URI and
`db_kwargs={"adbc.redis.tls_ca_file": "/worker/local/ca.crt"}`; the file must
exist at that path on every node. Use a worker-reachable endpoint, matching
native builds and a trusted Ray cluster: connection configuration can contain
credentials, and descriptors can contain SQL literals and bound values.
Named ACL users may need a positive, deployment-guaranteed
`partition_scan_cap`; the [partition permission contract](partitioned-reads.md)
also describes Search readiness and metadata permissions.

Reader, cursor and connection contexts close on completion, errors and
cooperative cancellation/unwinding. Abrupt worker termination cannot guarantee
Python cleanup; Redis cursor expiry remains the fallback. A failed attempt can
have consumed batches before its failure; follow Ray's task retry behavior and
avoid side effects in downstream processing that assumes exactly-once delivery.

See the [worker validation record](sdlc/changes/ray-worker-164/validation.md).
This is a repository adapter, not an upstream Ray integration or endorsement.
The `ray` extra and helper require a build containing this change; an older
published wheel will not supply them.

### Existing SQL reader and writer

Install `ray[data]` alongside `adbc-driver-redis[dbapi]`. Ray's
[`read_sql`](https://docs.ray.io/en/latest/data/api/doc/ray.data.read_sql.html)
accepts a factory that opens a fresh DB-API connection for each task:

```python
from functools import partial
import ray
from adbc_driver_redis import dbapi

ray.init(num_cpus=2, include_dashboard=False)
connect = partial(dbapi.connect, "redis://localhost:6379/0")
try:
    ds = ray.data.read_sql("SELECT 1 AS one", connect, override_num_blocks=1)
    print(ds.take_all())  # [{'one': 1}]
finally:
    ray.shutdown()
```

`Dataset.write_sql("INSERT INTO existing_table (id) VALUES (?)", connect)`
also works through the driver's `executemany`. Create the destination table
first. Writes autocommit; failed or retried tasks can leave partial writes or
duplicates. This integration does not provide transactional or exactly-once writes.

To retain the driver's Arrow types and avoid Ray's SQL reader converting rows
through Python objects, fetch with `cur.fetch_arrow_table()` and pass the table
to `ray.data.from_arrow(table)`. This materializes the query result on the
calling process before Ray distributes it; it is not a distributed database read.

On 2026-10-29, local Ray 2.59.0 with Python 3.12.14, PyArrow 26.0.0 and
ADBC driver manager 1.13.0 passed all three opt-in checks against Redis Software 8.6.2 with verified
active 4x QPF:
parameterized SQL reads with filters, NULLs and aggregates; a 300-row SQL write
round trip; and Arrow ingestion, fetch and Ray batch transformation. SQL reads
used one read task. Multiple input write blocks do not guarantee concurrent
write tasks because Ray may combine them. Multi-node execution, SQL read sharding
and performance were not validated. On a remote cluster, install the driver
and its native library on every worker and use a Redis endpoint reachable by
all workers; a client-local library path or `localhost` URI will not suffice.

## Apache Airflow

The runnable [TaskFlow smoke DAG](../python/examples/airflow_redis_smoke.py) uses
Airflow 3's public `airflow.sdk` API and the driver's DB-API wrapper. It bulk
loads four Arrow rows, then opens a fresh connection in a downstream task to
validate grouped SQL totals and a bound filter. Only table names and small
summaries pass through XCom. Connections and cursors open inside tasks; install
the driver and PyArrow on every worker.

The DAG uses a table name derived from its run ID, replaces that run's fixture
on load retries, and drops it in an `all_done` cleanup task. Run it only against
a disposable Redis database. This smoke test uses
[`dag.test()`](https://airflow.apache.org/docs/apache-airflow/3.3.2/core-concepts/debug.html)
with local task execution; it does not validate a scheduler, remote executor,
web UI, or distributed worker deployment.

Reproduce from the repository root using a separate **Python 3.12** environment
(Airflow and Superset have different dependency requirements):

```sh
python3.12 -m venv /tmp/redis-airflow-tests
source /tmp/redis-airflow-tests/bin/activate
constraints=https://raw.githubusercontent.com/apache/airflow/constraints-3.3.2/constraints-3.12.txt
python -m pip install 'apache-airflow==3.3.2' \
  'apache-airflow-providers-apache-arrow==0.1.1' --constraint "$constraints"
python -m pip install 'apache-airflow==3.3.2' \
  'apache-airflow-providers-apache-arrow==0.1.1' './python[dbapi]' pytest
make -C go build
export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so" # .dylib on macOS
RUN_AIRFLOW_TESTS=1 REDIS_URI=redis://localhost:6379/0 \
  python -m pytest -q python/tests/test_airflow.py
```

The second install keeps Airflow/provider versions pinned while allowing the
driver's newer PyArrow minimum beyond Airflow's optional-package constraint.
The test creates an isolated SQLite Airflow metadata database, runs the whole
DAG twice, and checks every task state so successful cleanup cannot mask a
failed upstream task. It also exercises the official Arrow provider's
[`AdbcHook`](https://airflow.apache.org/docs/apache-airflow-providers-apache-arrow/0.1.1/_api/airflow/providers/apache/arrow/hooks/adbc/index.html):
table creation, batched Arrow inserts, parameterized aggregation, and cleanup.
Ordinary Python test runs skip these tests unless `RUN_AIRFLOW_TESTS=1`.

### Redis hook for get_records()

Install `adbc-driver-redis[airflow]` in each Airflow task environment. Use the
package's `RedisAdbcHook` for Redis queries:

```python
from adbc_driver_redis.airflow import RedisAdbcHook

hook = RedisAdbcHook(adbc_conn_id="redis_analytics")
records = hook.get_records("SELECT $1 AS value", parameters=(42,))
assert records == [(42,)]
```

Create an Airflow connection named `redis_analytics` with type `adbc` and extras:

```json
{
  "dialect": "redis",
  "db_kwargs": {"uri": "redis://localhost:6379/0"}
}
```

Use `rediss://` for TLS and put native database options alongside `uri` in
`db_kwargs`. Keep credentials in Airflow's connection or secrets configuration.
The hook resolves the Redis library bundled in the wheel, including macOS, and
honors `REDIS_ADBC_LIBRARY` for development. It always loads the Redis driver;
the generic Arrow hook's `driver` extra is unnecessary.

The generic `AdbcHook` in Arrow provider 0.1.1 opens connections with
`autocommit=False`; its `get_records()` also requests transaction mode. Redis
rejects that mode even for a SELECT. `RedisAdbcHook` opens connections in
autocommit mode and defaults `run()`, `get_records()` and `get_first()` to it.
Explicit `run(..., autocommit=False)` and disabling autocommit are rejected.
For inherited `insert_rows()`, pass `autocommit=True`; each write takes effect
immediately and a later failure cannot roll it back. Use native `$1`, `$2`, …
placeholders for bound queries.

This is an explicitly imported hook, rather than a registered Airflow provider.
Selecting an `adbc` connection in `SQLExecuteQueryOperator` still selects the
generic Arrow hook. The example above calls the Redis hook inside a Python task.

The focused tests use Airflow 3.3.2, Arrow provider 0.1.1 and common-sql 2.1.1 in
a separate Python 3.12 environment:

```sh
python -m pip install 'apache-airflow==3.3.2' './python[airflow]' pytest
REDIS_URI=redis://localhost:6379/0 python -m pytest -q python/tests/test_airflow_hook.py
```

## dlt

Install `python -m pip install 'adbc-driver-redis[dlt]'`. This repository ships
`adbc_driver_redis.dlt.redis_adbc`, an append-only custom destination tested with
dlt 1.31.0. It sends normalized Parquet Arrow batches to the native driver's
`adbc_ingest` API; Redis Search must be running separately.

```python
import os
import dlt
from adbc_driver_redis.dlt import redis_adbc

pipeline = dlt.pipeline(
    pipeline_name="redis_orders",
    destination=redis_adbc(
        uri=os.environ["REDIS_URI"],
        schema_name="analytics",
        batch_size=1000,
    ),
)
info = pipeline.run(
    [{"id": 1, "amount": 10.5}, {"id": 2, "amount": 20.0}],
    table_name="orders",
    write_disposition="append",
)
info.raise_on_failed_jobs()
```

Query the result with `SELECT * FROM analytics.orders` through the driver.
The runnable [example](../python/examples/dlt_pipeline.py) loads and reads back
orders. Every invocation appends its input again.

`uri` can instead come from `DESTINATION__REDIS_ADBC__URI` or dlt's secrets file:

```toml
[destination.redis_adbc]
uri = "rediss://user:password@host:port/0"
```

In that case construct `redis_adbc(schema_name="analytics")` without `uri`.
TLS verification follows the driver's `rediss://` behavior. `schema_name`
defaults to `public` and is independent of dlt's `dataset_name`. The schema and
missing tables are created automatically. dlt normalizes table/column names with
`snake_case`; its internal tables and columns are omitted. Configure `db_kwargs`
for native database options and `index_columns` for ingestion indexing (`*` by
default). Batches default to 1,000 rows, and load jobs run sequentially. Keep
`loader_file_format="parquet"` and a positive `batch_size`.

Only `append` is implemented. `replace` and `merge` fail as terminal load errors
before opening a Redis connection. Primary-key hints do not create constraints
or upsert rows. Existing tables must accept the incoming columns and types;
apply SQL schema migrations yourself before loading changed resource schemas.
Empty resources do not create tables. Nested objects follow dlt's default custom
destination normalization (`max_table_nesting=0`, JSON serialized by its Parquet
writer); this integration does not add Redis JSON storage.

Writes autocommit without rollback. A failed batch can partially apply, and a
retry can duplicate those rows. A process failure after ingestion but before
dlt saves its checkpoint can also replay the batch. Successful checkpointed
batches are skipped on a resumed load. Serialize independent pipelines writing
to the same table. Keep or back up the local pipeline directory: custom
destinations do not restore incremental source state from Redis. See dlt's
[custom destination contract](https://dlthub.com/docs/dlt-ecosystem/destinations/destination)
for checkpoint, retry and state limitations.

## Validation

```sh
REDIS_URI=redis://localhost:6379/0 python -m pytest -q python/tests/
REDIS_URI=redis://localhost:6379/0 python -m pytest -q python/tests/test_dask.py
QUERY_FARM_TELEMETRY_OPT_OUT=1 RUN_DUCKDB_TESTS=1 \
  REDIS_URI=redis://localhost:6379/0 python -m pytest -qs python/tests/test_duckdb.py
python -m pip install 'ray[data]'
RUN_RAY_TESTS=1 REDIS_URI=redis://localhost:6379/0 \
  python -m pytest -qs python/tests/test_ray.py
RUN_RAY_TESTS=1 RAY_USAGE_STATS_ENABLED=0 REDIS_URI=redis://localhost:6379/0 \
  python -m pytest -q python/tests/test_ray_datasource.py
```

The DuckDB command downloads a native community extension. Ordinary test runs
skip that download unless explicitly enabled. Ray tests start a local two-CPU
runtime only when `RUN_RAY_TESTS=1`; their uniquely named tables are dropped
after each check. Superset smoke tests use a separate
Superset environment and a disposable metadata directory; see the change's
[verification record](sdlc/changes/271-280/validation.md).

For Superset 6.1's optional smoke test, use a separate Python 3.12 environment
(the app requires SQLAlchemy 1.4 and greenlet that does not build on Python 3.14).
Install `./python[superset]` and `pytest>=9.1.1`, then run
`RUN_SUPERSET_TESTS=1 REDIS_URI=redis://localhost:6379/0 python -m pytest -q python/tests/test_sqlalchemy.py python/tests/test_superset.py`. The test creates a
temporary metadata database; it does not use an existing Superset installation.
Superset 6.1 constrains older dependencies with known advisories; see the
[dependency audit](sdlc/changes/dependency-refresh-2026-10/python-advisories.json).
