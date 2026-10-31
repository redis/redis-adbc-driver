# Redis integration guide for dlt

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

## dlt

Install `python -m pip install 'adbc-driver-redis[dlt]'`. This repository ships
`adbc_driver_redis.dlt.redis_adbc`, an append-only custom destination tested with
dlt 1.31.0. It sends normalized Parquet Arrow batches to the native driver's
`adbc_ingest` API; Redis Search must be running separately.

```python
import os
from uuid import uuid4
import dlt
from adbc_driver_redis import dbapi
from adbc_driver_redis.dlt import redis_adbc

schema = "dlt_guide_" + uuid4().hex
pipeline = dlt.pipeline(
    pipeline_name=schema,
    destination=redis_adbc(uri=os.environ["REDIS_URI"], schema_name=schema, batch_size=1000),
)
with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
    try:
        info = pipeline.run(
            [{"id": 1, "amount": 10.5}, {"id": 2, "amount": 20.0}],
            table_name="orders", write_disposition="append",
        )
        info.raise_on_failed_jobs()
        cur.execute(f'SELECT id, amount FROM "{schema}".orders ORDER BY id')
        assert cur.fetchall() == [(1, 10.5), (2, 20.0)]
    finally:
        cur.execute(f'DROP SCHEMA IF EXISTS "{schema}" CASCADE')
```

The example queries its unique schema, checks both rows and removes that schema.
The runnable [example](https://github.com/redis/redis-adbc-driver/blob/main/python/examples/dlt_pipeline.py) loads and reads back
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

## Rerun this guide

From the repository root, with the guide's dependencies and native driver
installed, set `REDIS_URI` to the dedicated local Software fixture and run:

```sh
python python/scripts/check_integration_guide.py dlt
```

This executes the guide's code blocks or its linked runnable example. For
Superset it exercises the documented backend workflow in an isolated application;
it does not automate the browser UI. See the
[per-guide results](../sdlc/changes/integration-guides-262/validation.md).

[All integration guides](README.md)
