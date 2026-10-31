# Redis integration guide for Apache Airflow

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

## Apache Airflow

The runnable [TaskFlow smoke DAG](https://github.com/redis/redis-adbc-driver/blob/main/python/examples/airflow_redis_smoke.py) uses
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

## Rerun this guide

From the repository root, with the guide's dependencies and native driver
installed, set `REDIS_URI` to the dedicated local Software fixture and run:

```sh
python python/scripts/check_integration_guide.py airflow
```

This executes the guide's code blocks or its linked runnable example. For
Superset it exercises the documented backend workflow in an isolated application;
it does not automate the browser UI. See the
[per-guide results](../sdlc/changes/integration-guides-262/validation.md).

[All integration guides](README.md)
