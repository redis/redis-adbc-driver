# Choose your tool and connect

Start here to install Redis ADBC. The driver requires a Redis RAM database with
Search (Query Engine). Redis Software is the default development target; Redis
Cloud and supported Redis Open Source deployments also work. Flex, Valkey and
Redis 7.2 derivatives are unsupported. See [server requirements](driver-reference.md#server-requirements)
for versions and ACL permissions. Set QPF to 4× where supported, with sufficient
server CPU capacity, for concurrent query performance. Connections use autocommit; transactions are
unsupported. Existing application HASHes need [copy or adoption](driver-reference.md#existing-hash-collections-redis-adbc)
before they appear as SQL tables.

## Install without building

For native clients such as DuckDB, use the [managed native installer](native-installation.md)
for automatic downloads, checksum verification, stable library paths, upgrades
and uninstall. The Python wheel path below remains the simplest Python setup.

Use Python 3.14 for current integrations; Python 3.12+ remains installable for
the separate Superset compatibility environment. Wheels include the native library for Linux x86-64/ARM64
(glibc 2.28+) and macOS ARM64 (macOS 12+). Windows, macOS Intel and musl Linux
have no published wheels. Installing a driver does not start a Redis server.

```sh
python -m pip install 'adbc-driver-redis[dbapi]'
python -m adbc_driver_redis doctor --offline
```

Set `REDIS_URI` in your application's environment or secret manager. Examples
below read it rather than embedding credentials. Use `redis://host:port/0` for
plaintext or `rediss://host:port/0` for verified TLS; credentials use
`redis[s]://username:password@host:port/0` with URI-encoded reserved characters.
Use the deployment's endpoint and port, rather than assuming 6379.

```sh
export REDIS_URI='redis://localhost:6379/0'
python -m adbc_driver_redis doctor
```

`doctor` returns 0 when requested checks pass, 1 on a diagnostic failure and 2
for invalid arguments. Without a URI it runs offline. `--offline` ignores
`REDIS_URI`; `--uri` overrides the environment but may expose credentials in
shell history/process arguments. Raw native errors and URI values are not printed.
Online checks open a normal driver connection, including its metadata setup and
recovery, then fetch and validate `SELECT 1`; they do not explicitly create tables
or ingest sample rows. Read/write timeouts are set to five seconds per command;
connection retries and URI settings can make the total run longer. A successful
plaintext connection does not test TLS. Use your actual `rediss://` endpoint to
check TLS, leaving certificate verification enabled.
For a private CA, add `--ca-file /path/to/ca.pem`; it applies the driver's explicit
trust-bundle option to the direct connection and both manifest discovery checks.

## Pick an integration

| Tool | Install/configure | Detailed guide |
|---|---|---|
| pandas / Polars | Install the `dbapi` extra plus pandas or Polars; use the bundled driver | [pandas](integrations/pandas.md), [Polars](integrations/polars.md) |
| SQLAlchemy | Install the `sqlalchemy` extra; use `redis+adbc://` or `rediss+adbc://` | [SQLAlchemy](integrations/sqlalchemy.md) |
| Superset | Install the `superset` extra in a separate compatible environment on every web and SQL Lab worker | [Superset](integrations/superset.md) |
| Dagster | Install the `dagster` extra; use RedisADBCIOManager or the documented ADBCResource path | [Dagster](integrations/dagster.md) |
| DuckDB | Choose the embedded read-only extension or adbc_scanner for catalogs/writes | [DuckDB](duckdb.md) |
| Airflow | Install the `airflow` extra in every task environment; use RedisAdbcHook | [Airflow](integrations/airflow.md) |
| Prefect / dlt | Install the corresponding extra and configure the task/destination | [Prefect](integrations/prefect.md), [dlt](integrations/dlt.md) |
| Ray Data / Dask | Install the corresponding extra in every worker environment | [Ray](integrations/ray.md), [Dask](integrations/dask.md) |
| dbt | Install/configure the separate Redis adapter | [dbt adapter](https://github.com/redis/redis-dbt-adapter/tree/main/v1) |
| Spark / Databricks | Custom Python integration, or evaluate a separate Flight SQL server and JDBC client | [Ecosystem comparison](data-engineering-adbc-support.md) |
| Kafka Connect / Flink / Beam / NiFi | Custom integration, or evaluate the tool's JDBC path with Flight SQL | [Ecosystem comparison](data-engineering-adbc-support.md) |
| Fabric / Power Query | Custom connector and gateway deployment required; native Windows wheels unavailable | [Ecosystem comparison](data-engineering-adbc-support.md) |
| Fivetran / Airbyte / Trino | Custom connectors or separate projects; installing an ADBC wheel alone is insufficient | [Ecosystem comparison](data-engineering-adbc-support.md) |
| Snowflake / BigQuery | External ingestion pipeline; driver installation does not create native warehouse federation | [Ecosystem comparison](data-engineering-adbc-support.md) |

The [dedicated integration guides](integrations/README.md) document runnable
examples, version requirements and limitations. The [22-tool ecosystem comparison](data-engineering-adbc-support.md)
records test evidence and distinguishes candidate paths from implemented helpers.
Repository capabilities may require a newer build than the latest published wheel;
check the relevant guide before assuming an installed release includes a helper.

## Python DataFrames and DB-API

Python clients using this package resolve the bundled library automatically;
they do not need manifests. Install pandas or Polars only when using that example.

```python
import os
from adbc_driver_redis import dbapi

with dbapi.connect(os.environ["REDIS_URI"]) as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT 1 AS one")
        print(cur.fetch_arrow_table())
```

```python
import os
import pandas as pd
from adbc_driver_redis import dbapi

with dbapi.connect(os.environ["REDIS_URI"]) as conn:
    print(pd.read_sql("SELECT 1 AS one", conn))
```

```python
import os
import polars as pl

print(pl.read_database_uri("SELECT 1 AS one", os.environ["REDIS_URI"], engine="adbc"))
```

To query application data, replace the constant query with a SQL table already
created, ingested or adopted through the driver. The default schema is `public`.

## SQLAlchemy and Superset

SQLAlchemy URLs add `+adbc` to the Redis scheme. This example preserves URI
encoding and TLS when converting an existing Redis URI:

```python
import os
from sqlalchemy import create_engine, text
from sqlalchemy.engine import make_url

url = make_url(os.environ["REDIS_URI"])
if url.drivername not in ("redis", "rediss"):
    raise ValueError("Expected redis:// or rediss://")
engine = create_engine(url.set(drivername=url.drivername + "+adbc"))
try:
    with engine.connect() as conn:
        print(conn.execute(text("SELECT 1 AS one")).all())
finally:
    engine.dispose()
```

In Superset, select the Redis ADBC database type registered by the installed
package. Enter `redis+adbc://host:port/0` or `rediss+adbc://host:port/0`, including
the deployment's credentials using Superset's connection/secret controls. Test
the connection, then select a schema/table and create a dataset. Install in the
application's environment, rather than a separate host Python environment.
See [version compatibility and limitations](python-integrations.md#sqlalchemy-and-superset).

## DuckDB and other native ADBC clients

Register the wheel's library for tools that load drivers by name:

```sh
python -m adbc_driver_redis install-manifest --user
python -m adbc_driver_redis doctor --check-manifests
```

The second command uses `REDIS_URI` and tests both `redis` and `rediss` driver
names against that same endpoint. It verifies discovery and query execution;
it tests TLS only if the endpoint URI uses `rediss://`.

Use `--venv` instead of `--user` for clients using an active Python environment's
manifest directory. A separate DuckDB process must search that location too;
otherwise use user registration or a shared custom manifest directory:

```sh
python -m adbc_driver_redis install-manifest --directory /path/to/manifests
export ADBC_DRIVER_PATH=/path/to/manifests
python -m adbc_driver_redis doctor --check-manifests
```

Manifests contain absolute paths into the installed package. Keep that environment
in place and rerun registration after moving, replacing or deleting it. A service
must run as the same user or receive `ADBC_DRIVER_PATH` in its own environment.

In DuckDB, after registration:

```sql
-- DuckDB 2.0 uses Query.Farm's signed extension repository.
SET allow_extension_repositories = 'allowed';
CREATE EXTENSION REPOSITORY IF NOT EXISTS query_farm
  WITH PREFIX 'https://duckdb-extensions.query.farm';
INSTALL adbc_scanner FROM query_farm;
LOAD adbc_scanner FROM query_farm;
ATTACH 'redis://localhost:6379/0' AS r (TYPE adbc, driver 'redis', READ_ONLY);
SELECT * FROM adbc_scan('r', 'SELECT 1 AS one');
DETACH r;
```

Use your client’s parameter support for credentials. See [DuckDB setup](duckdb.md)
for telemetry settings, writes, schema names and version limits. The standalone
[`redis_adbc` extension](../duckdb-extension/README.md) embeds the driver and needs
no manifest, and supports SELECT only. Its registry submission has merged; consult the
extension guide for the current DuckDB version and build/distribution requirements.
Do not assume its commands are interchangeable with `adbc_scanner`.

For native-only installations, download the matching archive and checksum from
[GitHub releases](https://github.com/redis/redis-adbc-driver/releases), verify
the SHA-256, extract into a stable directory, then run `./install.sh --user` there.
The native installer requires Python 3.12+ without additional Python packages.
The Python `doctor` command requires the Python package; it is not in native tarballs.

For a managed installation, use [install, upgrade and uninstall](native-installation.md)
instead of unpacking into a temporary directory. The existing tarball installer
remains available for manual library placement.

## Airflow

Use the package's `RedisAdbcHook` and the [dedicated Airflow guide](integrations/airflow.md),
which covers provider compatibility, autocommit handling, worker installation,
connection extras and a runnable TaskFlow DAG. Generic AdbcHook defaults need
special care; use the Redis helper for the documented get_records() behavior.
