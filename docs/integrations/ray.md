# Redis integration guide for Ray Data

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

## Ray Data

### Worker-side Arrow reader

Complete the [shared example setup](example-setup.md) for the sample-data helper
before running the first block. It creates and removes a unique input table.

The source-tree `ray` extra pins Ray 2.59.0. Install this build on the caller
and every worker (Python 3.12+), including its matching native library:

```sh
python -m pip install './python[ray]'
```

```python
import os
import ray
from integration_guide_data import example_table
from adbc_driver_redis.ray import read_query

ray.init(num_cpus=2, include_dashboard=False)
try:
    with example_table() as name:
        ds = read_query(
            os.environ["REDIS_URI"], f'SELECT id, label FROM "{name}" WHERE id >= ?',
            parameters=(1,), partition_count=2, concurrency=2, batch_rows=65536,
        )
        rows = sorted(ds.take_all(), key=lambda row: row["id"])
        assert rows == [{"id": 1, "label": "alpha"}, {"id": 2, "label": None}, {"id": 3, "label": "雪"}]
        print(rows)

        # Persist the tool's computed output, then read it through a fresh connection.
        from integration_guide_data import write_result
        import pyarrow as pa
        write_result(pa.Table.from_pylist(rows))
finally:
    ray.shutdown()
```

`read_query` eagerly plans a supported SELECT through `ExecutePartitions` and
closes the planning handles. Ray read tasks open independent connections and
consume `ReadPartition` Arrow batches incrementally. Neither Python rows nor the
complete result are materialized on the caller. Empty reads retain the planned
Dataset schema; Ray may omit zero-row blocks from `to_arrow_refs()`.

The [live partition SQL subset](https://github.com/redis/redis-adbc-driver/blob/main/docs/partitioned-reads.md#supported-queries) applies.
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
`partition_scan_cap`; the [partition permission contract](https://github.com/redis/redis-adbc-driver/blob/main/docs/partitioned-reads.md)
also describes Search readiness and metadata permissions.

Reader, cursor and connection contexts close on completion, errors and
cooperative cancellation/unwinding. Abrupt worker termination cannot guarantee
Python cleanup; Redis cursor expiry remains the fallback. A failed attempt can
have consumed batches before its failure; follow Ray's task retry behavior and
avoid side effects in downstream processing that assumes exactly-once delivery.

See the [worker validation record](https://github.com/redis/redis-adbc-driver/blob/main/docs/sdlc/changes/ray-worker-164/validation.md).
This is a repository adapter, not an upstream Ray integration or endorsement.
The `ray` extra and helper require a build containing this change; an older
published wheel will not supply them.

### Existing SQL reader and writer

Install `ray[data]` alongside `adbc-driver-redis[dbapi]`. Ray's
[`read_sql`](https://docs.ray.io/en/latest/data/api/doc/ray.data.read_sql.html)
accepts a factory that opens a fresh DB-API connection for each task:

```python
import os
from uuid import uuid4
from functools import partial
import ray
from adbc_driver_redis import dbapi

ray.init(num_cpus=2, include_dashboard=False)
connect = partial(dbapi.connect, os.environ["REDIS_URI"])
name = "guide_ray_sql_" + uuid4().hex
try:
    ds = ray.data.read_sql("SELECT 1 AS one", connect, override_num_blocks=1)
    with connect() as conn, conn.cursor() as cur:
        try:
            cur.execute(f'CREATE TABLE "{name}" (one bigint)')
            ds.write_sql(f'INSERT INTO "{name}" (one) VALUES (?)', connect)
            cur.execute(f'SELECT one FROM "{name}"')
            assert cur.fetchall() == [(1,)]
            print("REDIS_ADBC_READ_WRITE_OK")
        finally:
            cur.execute(f'DROP TABLE IF EXISTS "{name}"')
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

## Rerun this guide

From the repository root, with the guide's dependencies and native driver
installed, set `REDIS_URI` to the dedicated local Software fixture and run:

```sh
python python/scripts/check_integration_guide.py ray
```

This executes the guide's code blocks or its linked runnable example. For
Superset it exercises the documented backend workflow in an isolated application;
it does not automate the browser UI. See the
[per-guide results](../sdlc/changes/integration-guides-262/validation.md).


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md)
