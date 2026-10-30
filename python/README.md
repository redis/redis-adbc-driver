# Redis ADBC driver

SQL queries and Arrow bulk ingestion over Redis through the Arrow Database
Connectivity (ADBC) API. This community driver bundles its native library in
platform wheels for Linux x86_64/arm64 (glibc 2.28+) and macOS arm64 (macOS 12+).
Use Python 3.14 for the current integrations. Python 3.12+ remains installable
for the separate Superset compatibility environment. Windows and macOS Intel wheels are not provided.

For installation and diagnostics, see [Choose your tool and connect](https://github.com/redis/redis-adbc-driver/blob/main/docs/setup.md).

```sh
python -m pip install 'adbc-driver-redis[dbapi]' polars
python -m adbc_driver_redis doctor --offline
```

**Recommended Redis configuration:** Redis Search (Query Engine) enabled
and query performance factor (QPF) set to **4×** where supported.
Search is required; 4× QPF is recommended for concurrent query performance.
Configure these on the Redis server; installing this package does not enable them.
See [server requirements](https://github.com/redis/redis-adbc-driver/blob/main/docs/driver-reference.md#server-requirements)
for configuration and CPU sizing guidance.

Set `REDIS_URI` and run `python -m adbc_driver_redis doctor` to check connectivity,
Search and a fetched `SELECT 1`. Add `--check-manifests` after registration for
native clients that load drivers by name.

With Redis and Search running:

```python
import polars as pl

df = pl.read_database_uri(
    query="SELECT 1 AS one",
    uri="redis://localhost:6379/0",
    engine="adbc",
)
print(df)
```

For direct DB-API use:

```python
from adbc_driver_redis import dbapi

with dbapi.connect("redis://localhost:6379/0") as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT 1 AS one")
        print(cur.fetch_arrow_table())
```

Use `rediss://` for TLS. Search is required; writes autocommit and transactions
are unsupported. SQL support comes from this driver. Installing the package
does not install or start a Redis server.

The `airflow` extra supports `adbc_driver_redis.airflow.RedisAdbcHook` for
autocommit queries through Airflow.

Optional extras: `sqlalchemy` for current SQLAlchemy, `superset` for the isolated
Superset 6.1 / SQLAlchemy 1.4 compatibility environment,
`dagster` for the Redis ADBC IO manager, and `dask` for partitioned DataFrame
reads with `adbc_driver_redis.dask.read_query`. The source build's `ray` extra
pins Ray 2.59.0 and supplies worker-side Arrow reads through
`adbc_driver_redis.ray.read_query`; see the
[Ray guide](https://github.com/redis/redis-adbc-driver/blob/main/docs/python-integrations.md#ray-data)
for installation on every worker and live-read consistency limits.

The `dlt` extra provides the append-only dlt destination:

```python
import dlt
from adbc_driver_redis.dlt import redis_adbc

pipeline = dlt.pipeline(
    pipeline_name="redis_orders",
    destination=redis_adbc(uri="redis://localhost:6379/0", schema_name="analytics"),
)
pipeline.run([{"id": 1, "amount": 10.5}], table_name="orders")
```

Install with `python -m pip install 'adbc-driver-redis[dlt]'`. Loads use Arrow
batches through ADBC. Only append is supported; retries can duplicate partially
written batches. Keep the local dlt pipeline directory for checkpoints.

See the [installation and integration guide](https://github.com/redis/redis-adbc-driver/blob/main/docs/python-integrations.md)
and [server requirements](https://github.com/redis/redis-adbc-driver/blob/main/docs/driver-reference.md#server-requirements)
for supported Redis deployments, configuration and limitations.

## Rerun candidate integration tests

The saved runner starts or reuses a disposable Redis 8.6.2 container with
Search on `127.0.0.1:16379`, builds the native driver, and runs the local
candidate suites for NumPy, scikit-learn, pandas, Polars, Dask, Ray Data,
DuckDB, Airflow, Dagster, Prefect, and dlt.
Use Python 3.14 as described above. DuckDB may download its ADBC scanner
extension from Query.Farm on the first run.

```sh
python3 -m venv .venv-candidates
.venv-candidates/bin/python -m pip install -e 'python[test,candidate-tests]'
PYTHON="$PWD/.venv-candidates/bin/python" python/scripts/run-candidate-tests.sh
```

The runner leaves the container available for another run. Remove it with
`docker rm -f redis-adbc-python-test`. Set `REDIS_TEST_PORT` and
`REDIS_TEST_CONTAINER` to use another local port and container name. The Airflow
end-to-end smoke test runs in a temporary metadata database. Flight SQL,
Spark/Databricks, hosted runtimes, and remote orchestration workers are outside
this local harness.

Arrow IPC stream/file serialization is available inside the native driver through
standard ADBC statement options. See the [IPC contract and example](https://github.com/redis/redis-adbc-driver/blob/main/docs/arrow-ipc.md).
