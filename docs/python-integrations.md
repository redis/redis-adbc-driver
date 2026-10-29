# Python analytics integrations

These integrations use the Redis ADBC library, not Redis's wire protocol as a
PostgreSQL server. Search is required. Writes autocommit; transaction scopes do
not provide rollback or isolation. Use a dedicated Redis database for tests.

## Install and discover the driver

Install from [PyPI](https://pypi.org/project/adbc-driver-redis/) (Python 3.10+;
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
and supports `--directory`. It requires Python 3.10+ but no Python packages.
The installer writes both `redis.toml` and `rediss.toml`, following the
[ADBC manifest specification](https://arrow.apache.org/adbc/current/format/driver_manifests.html).
Paths are absolute: keep the library in place and rerun installation after moving
or replacing it. User installation defaults to `~/.config/adbc/drivers` on Linux
(`XDG_CONFIG_HOME` is honored) or `~/Library/Application Support/ADBC/Drivers`
on macOS. No privileged install is performed automatically.

With manifests installed, Python's manager also accepts
`dbapi.connect(uri="redis://localhost:6379/0", autocommit=True)` or
`driver="redis"`. Older managers may need a library path; the package requires
manager >=1.8 and is tested with 1.12.0.

## SQLAlchemy and Superset

Install the wheel's `sqlalchemy` extra in the application environment. The dialect
supports SQLAlchemy 1.4 (Superset 6) and 2.x. It compiles PostgreSQL SQL and reflects
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

## Validation

```sh
REDIS_URI=redis://localhost:6379/0 python -m pytest -q python/tests/
QUERY_FARM_TELEMETRY_OPT_OUT=1 RUN_DUCKDB_TESTS=1 \
  REDIS_URI=redis://localhost:6379/0 python -m pytest -qs python/tests/test_duckdb.py
```

The second command downloads a native community extension. Ordinary test runs
skip that download unless explicitly enabled. Superset smoke tests use a separate
Superset environment and a disposable metadata directory; see the change's
[verification record](sdlc/changes/271-280/validation.md).

For Superset 6.1's optional smoke test, use a separate Python 3.12 environment
(the app requires SQLAlchemy 1.4). Install `apache-superset==6.1.0`,
`flask-caching==2.3.1`, `cachetools`, `./python[sqlalchemy]` and `pytest`, then run
`RUN_SUPERSET_TESTS=1 REDIS_URI=redis://localhost:6379/0 python -m pytest -q python/tests/test_sqlalchemy.py python/tests/test_superset.py`. The test creates a
temporary metadata database; it does not use an existing Superset installation.
