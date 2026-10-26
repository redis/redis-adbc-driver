# Redis ADBC driver

SQL queries and Arrow bulk ingestion over Redis through the Arrow Database
Connectivity (ADBC) API. This community driver bundles its native library in
platform wheels for Linux x86_64/arm64 (glibc 2.28+) and macOS arm64 (macOS 12+).
Use Python 3.14 for the current integrations. Python 3.12+ remains installable
for the separate Superset compatibility environment. Windows and macOS Intel wheels are not provided.

```sh
python -m pip install 'adbc-driver-redis[dbapi]' polars
```

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

Optional extras: `sqlalchemy` for current SQLAlchemy, `superset` for the isolated
Superset 6.1 / SQLAlchemy 1.4 compatibility environment,
`dagster` for the Redis ADBC IO manager, and `dask` for partitioned DataFrame
reads with `adbc_driver_redis.dask.read_query`.

See the [installation and integration guide](https://github.com/redis/redis-adbc-driver/blob/main/docs/python-integrations.md)
and [server requirements](https://github.com/redis/redis-adbc-driver/blob/main/docs/driver-reference.md#server-requirements)
for supported Redis deployments, configuration and limitations.

Arrow IPC stream/file serialization is available inside the native driver through
standard ADBC statement options. See the [IPC contract and example](https://github.com/redis/redis-adbc-driver/blob/main/docs/arrow-ipc.md).
