# Redis integration guide for Prefect

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

## Prefect custom Python tasks

Install `pip install 'adbc-driver-redis[prefect]'`. Create connections inside each
task so concurrent tasks and retries each own their connection and cursor:

```python
import os
from adbc_driver_redis import dbapi
from integration_guide_data import write_result
from prefect import flow, task
from prefect.cache_policies import NO_CACHE

@task(cache_policy=NO_CACHE, persist_result=False)
def query(uri):
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute("SELECT 1 AS one")
        output = cur.fetch_arrow_table()
    return write_result(output)

@flow(persist_result=False)
def pipeline(uri):
    return query.submit(uri).result()

print(pipeline(os.environ["REDIS_URI"]).to_pylist())
```

The [Prefect tests](https://github.com/redis/redis-adbc-driver/blob/main/python/tests/test_prefect.py) run real local flows with
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

## Rerun this guide

From the repository root, with the guide's dependencies and native driver
installed, set `REDIS_URI` to the dedicated local Software fixture and run:

```sh
python python/scripts/check_integration_guide.py prefect
```

This executes the guide's code blocks or its linked runnable example. For
Superset it exercises the documented backend workflow in an isolated application;
it does not automate the browser UI. See the
[per-guide results](../sdlc/changes/integration-guides-262/validation.md).

[All integration guides](README.md)
