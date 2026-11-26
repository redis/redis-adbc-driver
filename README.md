<img referrerpolicy="no-referrer-when-downgrade" src="https://static.scarf.sh/a.png?x-pxid=1e10821f-4aa3-466e-8539-4ae9a1f7c571" />

<!--
  Copyright (c) 2026 ADBC Drivers Contributors

  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

          http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.
-->

# Arrow Database Connectivity (ADBC) driver for Redis

Use SQL to read and write Redis HASHes, fetch typed Apache Arrow results, and
connect Redis to data tools through [ADBC](https://arrow.apache.org/adbc/current/format/specification.html).
Redis Search (Query Engine) provides indexing; the driver supplies SQL execution,
Arrow conversion, and table management. Use the
[official Redis clients](https://redis.io/docs/latest/develop/clients/) for native
Redis commands; add ADBC when your workflow needs SQL or Arrow.

Choose whether the driver owns your data, your indexes, or both. You can keep
ordinary application HASHes directly accessible to Redis clients while querying
and updating those same HASHes through SQL.

[Documentation](https://redis.github.io/redis-adbc-driver/) ·
[Setup and installation](docs/setup.md) · [SQL reference](docs/driver-reference.md) ·
[Build from source](docs/building.md)

Connections require RESP3, Redis Functions, and an explicit ACL username and password.
Password-only connections and EVAL/EVALSHA compatibility fallbacks are unsupported.
See [connection and ACL requirements](docs/driver-reference.md#acl-users).

## Contents

- [How tables and snapshots work](#how-tables-and-snapshots-work)
- [Quick start](#quick-start)
- [Data tools and integration status](#data-tools-and-integration-status)
- [Performance benchmarks](#performance-benchmarks)
- [Compatibility and validation](#compatibility-and-validation)
- [Apache Arrow feature matrix](#apache-arrow-feature-matrix)
- [Redis Software feature matrix](#redis-software-feature-matrix)
- [Usage analytics](#usage-analytics)
- [Development](#development)

## How tables and snapshots work

The driver has **three table modes**. The main difference is who owns the HASHes
and who maintains the Search indexes.

| | Managed (default) | Live HASH tables | Unmanaged |
|---|---|---|---|
| HASH owner | Driver | Application | Application |
| Index manager | Driver | Driver | Application |
| SQL reads and writes | Yes | Yes, on the original HASHes | Yes, through a registered application index |
| Redis-style reads | Experimental snapshot-aware [NativeClient](docs/native-client.md) | Yes, through ordinary Redis clients | Yes, through ordinary Redis clients |
| Direct Redis writes | Unsupported; use SQL or Arrow | Yes | Yes |
| Read consistency | Statement snapshots | Live source values | Live source values |
| SQL write publication | Supported writes publish atomically | Per-row writes; a statement can partially succeed | Source writes can partially succeed |
| Copy of application data | Import creates a private copy | No copy | No copy |
| Arrow ingestion | Supported | Supported within the [live write contract](docs/live-hash-tables.md#write-arrow-data-to-live-hashes) | Supported within the unmanaged write contract |

The experimental native HASH client provides managed-table reads without SQL
translation. It is included in v0.0.29, uses internal `__rowid` values, and is
read-only. Ordinary Redis reads of physical managed keys do not resolve table
snapshots, row versions, or tombstones.

**Choose managed tables** when the driver should own both data and indexes.
They are the default for SQL/Arrow pipelines and consistent analytical reads.
Supported writes go through the driver; importing application HASHes creates a
private copy that needs explicit refresh. See the
[managed-table walkthrough](docs/sql-redis-example.md) and
[snapshot contract](docs/snapshots.md).

**Choose live HASH tables** when the application should own the data and the
driver should own the indexes. SQL and Redis clients operate on the original
HASHes; the driver creates and manages its Search index. This mode is included in v0.0.30 and later. See the [live HASH guide](docs/live-hash-tables.md) for
registration, SQL/Redis examples, automatic keys, and limits. Current source builds
also support named [live schemas](docs/live-hash-tables.md#live-schemas-and-sql-ddl)
with SQL `CREATE`/`DROP`/`ALTER SCHEMA`, and `CREATE TABLE` (including `AS
SELECT`), `DROP TABLE` (unregister, keeping HASHes) and `ALTER TABLE … RENAME TO`
for live tables.

Live Arrow ingestion supports `create`, `append`, `create_append`, and `replace`.
Creation requires an explicitly configured empty application prefix. Replacement
requires exact target/index consent and stopped application/driver writers and
DDL users; it removes old HASHes and TTLs and resets the index. Live writes can partially
succeed, and replacement has no whole-call rollback. See the
[live write contract](docs/live-hash-tables.md#write-arrow-data-to-live-hashes)
for configuration and recovery.

**Choose unmanaged tables** when the application should own both data and indexes.
Register an existing compatible Search index for SQL access to the original
HASHes. SQL and Redis clients operate on the same data; the application controls
index configuration and lifecycle. See the
[unmanaged guide](docs/unmanaged-tables.md) for registration, examples, and limits.

All modes use autocommit; multi-statement transactions and rollback are unsupported.
Unmanaged and live tables have no managed row snapshot: concurrent SQL and Redis
changes can interleave, and read-modify-write updates can lose intervening changes.
Direct Redis writes bypass SQL validation.

See [managed snapshots](docs/snapshots.md) for storage compatibility and fixed limits,
or the [ownership comparison](docs/managed-vs-unmanaged-tables.md) for more detail.

## Quick start

This walkthrough builds the current source, starts a disposable Redis Software
fixture, and queries sample managed tables.

You need Git, Make, the Go toolchain declared in [go/go.mod](go/go.mod), a C compiler,
Python 3.14+, Docker with Compose, OpenSSL, `curl`, and `jq`. For a packaged release instead,
use [installation without building](docs/setup.md#install-without-building).
Published packages can lag the capabilities described by this source checkout.

### 1. Build and install

```bash
git clone --depth 1 https://github.com/redis/redis-adbc-driver.git
cd redis-adbc-driver
make -C go build adbc-cli

python3 -m venv .venv
. .venv/bin/activate
python -m pip install -e './python[dbapi]'

case "$(uname -s)" in
  Darwin) export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" ;;
  Linux)  export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so" ;;
esac
```

`REDIS_ADBC_LIBRARY` makes the Python wrapper use the library you just built.
`go/build/redis-adbc` is the command-line tool for inspecting, importing, and
registering HASH collections. See [building from source](docs/building.md) for
platform details and [native installation](docs/native-installation.md) for other
applications.

### 2. Start Redis Software

```bash
make -C go redis-up
source go/build/test.env
```

Setup starts an authenticated TLS Redis Software database with Search and verifies
active **4× Query Performance Factor (QPF)**. Source the generated environment file
in each terminal used for the examples; it supplies the endpoint, credentials, and
certificate trust. Keep it private.

Use a fresh, dedicated database. The sample loader replaces `customers` and `sales`;
legacy driver catalogs have no in-place upgrade path.

### 3. Load the sample dataset

```bash
python go/examples/load_sample.py
```

This creates `public.customers` with 50 rows through SQL and `public.sales` with
10,000 rows through Arrow ingestion. Pass `--rows N` to change the sales row count.

### 4. Run SQL and fetch Arrow results

```bash
python go/examples/sql.py "SELECT COUNT(*) FROM sales"
python go/examples/sql.py
```

The second command opens an interactive prompt. End each statement with `;`:

```sql
SELECT country, COUNT(*) AS orders, SUM(quantity) AS units
FROM sales
GROUP BY country
ORDER BY units DESC;

SELECT c.name, COUNT(*) AS orders
FROM sales s JOIN customers c ON s.customer_id = c.customer_id
WHERE s.status = 'shipped'
GROUP BY c.name
ORDER BY orders DESC, c.name
LIMIT 5;
```

From your own Python code:

```python
import os
from adbc_driver_redis import dbapi

with dbapi.connect(os.environ["REDIS_URI"]) as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT country, quantity FROM sales WHERE quantity >= 18")
        table = cur.fetch_arrow_table()
        print(table)
```

Continue with [sample queries](docs/sample-queries.md) for subqueries, CTEs, window
functions, date/time expressions, views, and writes.

### 5. Clean up

When finished with the examples:

```bash
make -C go redis-down
```

## Data tools and integration status

| Workflow | Guides |
|---|---|
| Python and DataFrames | [PyArrow](docs/integrations/pyarrow.md), [pandas](docs/integrations/pandas.md), [Polars](docs/integrations/polars.md) |
| SQL exploration and BI | [DuckDB](docs/duckdb.md), [SQLAlchemy](docs/integrations/sqlalchemy.md), [R / DBI](docs/integrations/r-dbi.md) |
| Orchestration | [Airflow](docs/integrations/airflow.md), [Dagster](docs/integrations/dagster.md), [Prefect](docs/integrations/prefect.md) |
| Pipelines and migration | [dlt](docs/integrations/dlt.md), [Sling](docs/integrations/sling.md), [dbt](docs/integrations/dbt.md) |
| Partitioned processing | [Dask](docs/integrations/dask.md), [Ray Data](docs/integrations/ray.md) |

Framework adapters for Airflow, Dagster, Dask, Ray, Prefect, dlt, and Superset live
in the separate [redis-adbc-integrations](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations)
project, whose releases share the driver's version and require that driver
release. Follow each guide for installation and compatibility limits; the core
driver package does not install those adapters. Superset 6.1 has a documented
[compatibility blocker](docs/integrations/superset.md).

The [integration index](docs/integrations/README.md) and
[ecosystem comparison](docs/data-engineering-adbc-support.md) distinguish native
connectors, custom adapters, local validation, and unverified deployment paths.
For Flight SQL or JDBC, see the separate
[Redis Arrow Flight server](https://github.com/redis/redis-apache-arrow-flight).

## Performance benchmarks

The [performance report](PERFORMANCE.md) records the SSB, TPC-H and partial
TPC-DS SF1 released-driver comparison with DuckDB, resource limits and query
issues. Separate focused development-driver follow-ups preserve the historical
baseline. Full benchmark suites remain stopped.

## Compatibility and validation

**Redis Search (Query Engine) and RESP3 are required.** Development and tests use
Redis Software with active, verified **4× QPF**. QPF is a server setting, separate
from driver worker settings, and does not promise a 4× speedup.

The driver implements a PostgreSQL-oriented SQL dialect; it is not fully PostgreSQL
compatible. See the [SQL reference](docs/driver-reference.md) and
[compatibility report](docs/compatibility.md) for supported operations, measured
coverage, exact server versions, and historical validation limits. That report
includes Redis Software, Redis Open Source, and Redis Cloud configurations;
historical results apply to their recorded server and settings.

[SQL SELECT grants](docs/sql-rbac.md) provide a restricted, administrator-configured
Redis Software role/ACL mapping. They do not implement general SQL privileges.

The experimental `adbc.redis.flex=true` stopgap has a separate
[scope and evidence](docs/redis-flex-stopgap.md). The new driver-index-owned live
mode is not supported on Flex.

## Apache Arrow feature matrix

See the [Arrow and ADBC matrix](docs/feature-matrices.md#apache-arrow-feature-matrix)
for types, ingestion, IPC, streaming, partitions, cancellation, and limits.
[Arrow interoperability](docs/arrow-interoperability.md),
[Substrait](docs/substrait.md), and [experimental CUDA execution](docs/gpu-execution.md)
describe their individual contracts. CPU execution is the default.

## Redis Software feature matrix

The [Redis Software matrix](docs/feature-matrices.md#redis-software-feature-matrix)
separates driver capabilities from server administration and companion services,
including authentication, TLS, cluster connectivity, recovery, and deployment limits.

## Usage analytics

[Product usage analytics](analytics/README.md) reports high-level statement types,
counts, timings, row counts, and documented source/deployment metadata. SQL text,
query parameters, results, and database contents are not collected. Reporting is
on by default, using `https://adbc-telemetry.sa.redislabs.com/v1/usage`.

Opt out with the database option `adbc.redis.telemetry.enabled=false`.

## Development

```bash
make -C go build adbc-cli
make -C go test-software
```

The test target verifies effective 4× QPF and `noeviction` through the management
API before running. Tests modify data; use a dedicated fixture. Concurrent
sessions need separate Compose project names and API/database ports.

Read [CLAUDE.md](CLAUDE.md), [building and testing](docs/building.md), and the
[development workflow](docs/sdlc/README.md). The Go implementation builds on
[driverbase-go](https://github.com/adbc-drivers/driverbase-go) and uses the
[ADBC validation suite](https://github.com/adbc-drivers/validation).
For native distribution and registration, see the
[managed installer](docs/native-installation.md) and [dbc guide](docs/dbc.md).
