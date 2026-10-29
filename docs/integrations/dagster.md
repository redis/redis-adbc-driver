# Redis integration guide for Dagster

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

The [smoke example](https://github.com/redis/redis-adbc-driver/blob/main/python/examples/dagster_smoke.py) materializes `orders`,
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

[All integration guides](README.md)
