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

Install from [PyPI](https://pypi.org/project/adbc-driver-redis/) (Python 3.14 or newer required;
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

The wheel bundles the native library. macOS release wheels target macOS 15+
(the oldest macOS branch receiving Apple's October 2026 security updates);
the builder checks the library deployment target
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
manager >=1.13.0.

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

The adapter, examples and tests moved to [redis-adbc-integrations](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/prefect.md).
Install it separately; adapter imports now use `redis_adbc_integrations`.

## SQLAlchemy and Superset

Install the wheel's `sqlalchemy` extra for SQLAlchemy >=2.1.9.

Superset 6.1 is unsupported on Python 3.14+. The Superset adapter moved to the
[redis-adbc-integrations guide](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/superset.md);
[#471](https://github.com/redis/redis-adbc-driver/issues/471) is closed, and no
Superset 7.x run is recorded in this repository.

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

For Superset, use the [moved guide](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/superset.md);
restoration issue [#471](https://github.com/redis/redis-adbc-driver/issues/471) is closed.

## Dagster

The adapter, examples and tests moved to [redis-adbc-integrations](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/dagster.md).
Install it separately; adapter imports now use `redis_adbc_integrations`.

## Dask

The adapter, examples and tests moved to [redis-adbc-integrations](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/dask.md).
Install it separately; adapter imports now use `redis_adbc_integrations`.

## Ray Data

### Worker-side Arrow reader

The adapter, examples and tests moved to [redis-adbc-integrations](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/ray.md).
Install it separately; adapter imports now use `redis_adbc_integrations`.

## Apache Airflow

### Redis hook for get_records()

The adapter, examples and tests moved to [redis-adbc-integrations](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/airflow.md).
Install it separately; adapter imports now use `redis_adbc_integrations`.

## Sling

[Sling](https://slingdata.io) loads the native Redis ADBC shared library directly;
it does not require a Python integration extra. PostgreSQL → Redis → PostgreSQL
passed twice with 10,003 rows and zero value or duplicate differences on verified
Redis Software 4x QPF. Redis-target writes require metadata template overrides,
a precreated table, and direct truncate-and-load. Default staging and empty Arrow
results have documented Sling limitations; this is not a performance benchmark.
See the [Sling guide](integrations/sling.md) for setup and the
[standalone test](../scripts/test_sling_postgres.py) for reproduction.

## dlt

The adapter, examples and tests moved to [redis-adbc-integrations](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/dlt.md).
Install it separately; adapter imports now use `redis_adbc_integrations`.

## Validation

Core Python and upstream ADBC scanner tests remain in this repository. Framework
contract suites, Ray TLS worker checks and their dependencies run in
[redis-adbc-integrations](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations). See the
[acceptance-suite audit and commands](integrations/acceptance.md) for additional
upstream driver compliance contracts.

```sh
make -C go redis-up build
export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so"
bash go/software-setup.sh --exec python -m pytest -q "$PWD/python/tests/"
```
