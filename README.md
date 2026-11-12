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

Query Redis with SQL, stream typed Apache Arrow results, and bulk-load Arrow data
through a native [ADBC](https://arrow.apache.org/adbc/current/format/specification.html)
driver. Redis HASHes store the rows; Redis Search (Query Engine) accelerates
supported filters, sorting and aggregations.

Use Redis ADBC when SQL and Arrow fit your application, analytics or data pipeline.
For native Redis commands and data structures, start with the
[official Redis clients](https://redis.io/docs/latest/develop/clients/).

[Documentation website](https://redis.github.io/redis-adbc-driver/) ·
[Choose your tool](docs/setup.md) · [Driver reference](docs/driver-reference.md)

## Contents

- [Why a Redis ADBC driver?](#why-a-redis-adbc-driver)
- [Quick start](#quick-start)
- [How tables and snapshots work](#how-tables-and-snapshots-work)
- [Data tools and integration status](#data-tools-and-integration-status)
- [Compatibility and validation](#compatibility-and-validation)
- [Apache Arrow feature matrix](#apache-arrow-feature-matrix)
- [Redis Software feature matrix](#redis-software-feature-matrix)
- [Usage analytics](#usage-analytics)
- [Development](#development)

## Why a Redis ADBC driver?

- **Use SQL for reads and writes.** Query individual records, join tables,
  aggregate data, and use supported inserts, updates, deletes and DDL.
- **Move typed data with Arrow.** Fetch record batches or bulk-ingest Arrow
  tables and streams, with explicit type and NULL handling.
- **Connect data tools.** Use Python, pandas, Polars, DuckDB and orchestration
  tools through their documented ADBC paths or repository adapters.
- **Bring application data into SQL.** Import eligible existing HASH collections
  into private managed tables, then refresh explicitly when the source changes.

## Quick start

This walkthrough installs a published Python wheel, starts a disposable Redis
Software database, and runs queries over sample data. You need Git, Docker with
Compose, Make, `curl`, `jq`, and Python (3.14 recommended; package minimum 3.12).

For an existing deployment or a different tool, use [Choose your tool and connect](docs/setup.md).
Use a fresh, dedicated database for this walkthrough: the sample loader replaces
any existing `customers` and `sales` tables. Legacy driver catalogs have no
in-place upgrade path; see [storage compatibility](docs/snapshots.md).

### 1. Install the driver

Clone the repository for the startup script and sample programs:

```bash
git clone --depth 1 https://github.com/redis/redis-adbc-driver.git
cd redis-adbc-driver
python3 -m venv .venv
. .venv/bin/activate
python -m pip install 'adbc-driver-redis[dbapi]==0.0.18'
```

The wheel bundles the native driver for macOS Apple Silicon (macOS 15+) and Linux
x86-64/ARM64 (glibc 2.28+). See [installation options](docs/setup.md#install-without-building)
for other tools, the [managed native installer](docs/native-installation.md) for
native applications, or [Building from source](docs/building.md).

This example uses published version 0.0.18. Feature documentation describes the
current source tree and may require a newer build than this wheel.

### 2. Start Redis Software

```bash
make -C go redis-up
source go/build/test.env
```

The setup creates a disposable, authenticated TLS Redis Software fixture with
Search and verifies active **4× QPF**. Source the generated environment file in
each terminal that runs the Python examples; it supplies the endpoint,
credentials and certificate trust. Keep that file private.

### 3. Load the sample dataset

```bash
python go/examples/load_sample.py
```

The loader creates `customers` (50 rows through SQL) and `sales` (10,000 rows
through Arrow ingestion) in the `public` schema. Use `--rows N` to change the
sales row count. The `email` and `notes` columns are stored without indexing.

### 4. Run SQL

Run one statement:

```bash
python go/examples/sql.py "SELECT COUNT(*) FROM sales"
```

Or open the interactive prompt and end each statement with `;`:

```bash
python go/examples/sql.py
```

```sql
SELECT order_id, country, product, quantity, unit_price
FROM sales
WHERE country = 'JPN' AND quantity >= 18
ORDER BY unit_price DESC
LIMIT 5;

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

Continue with [sample SQL queries](docs/sample-queries.md) for subqueries, CTEs,
window functions, date/time expressions, views and writes. To query from your own
code, follow the [Python DB-API and DataFrame examples](docs/setup.md#python-dataframes-and-db-api).

### 5. Clean up

```bash
make -C go redis-down
```

## How tables and snapshots work

**Managed tables are the default for SQL and Arrow workflows.** Write through
the driver using SQL or supported Arrow ingestion. Each statement reads one
committed generation, including streamed results, and supported writes publish
atomically. Connections use autocommit; multi-statement transactions and rollback
are unsupported.

Read managed tables through SQL/ADBC. Ordinary Redis reads of physical HASHes or
indexes do not resolve snapshot generations, row versions or tombstones, and
direct writes bypass the storage protocol. The experimental
[native client wrapper](docs/native-client.md) provides supported HASH-style reads
through the snapshot reader; it requires a source build and internal `__rowid`
values. Application-key lookup and `FT.SEARCH` are not implemented by the wrapper.

Existing application HASHes remain application-owned. Adoption and foreign-index
imports create private managed copies; refresh explicitly to capture later
source changes. Compare [managed and application-owned data](docs/managed-vs-unmanaged-tables.md#choose-by-use-case)
for ownership, freshness and storage tradeoffs.

See the [snapshot contract](docs/snapshots.md) for storage compatibility and fixed
limits, the [architecture guide](docs/driver-reference.md#architecture-hybrid-index-row-layout)
for layout, and the [SQL INSERT and SELECT walkthrough](docs/sql-redis-example.md)
for snapshot-aware storage examples.

## Data tools and integration status

Choose a path, then follow its guide for installation, examples and validation limits:

| Workflow | Guides |
|---|---|
| Python and DataFrames | [PyArrow](docs/integrations/pyarrow.md), [pandas](docs/integrations/pandas.md), [Polars](docs/integrations/polars.md) |
| SQL exploration and BI | [DuckDB](docs/duckdb.md), [SQLAlchemy](docs/integrations/sqlalchemy.md), [Superset](docs/integrations/superset.md), [R / DBI](docs/integrations/r-dbi.md) |
| Orchestration | [Airflow](docs/integrations/airflow.md), [Dagster](docs/integrations/dagster.md), [Prefect](docs/integrations/prefect.md) |
| Pipelines and migration | [dlt](docs/integrations/dlt.md), [Sling](docs/integrations/sling.md), [dbt](docs/integrations/dbt.md) |
| Partitioned processing | [Dask](docs/integrations/dask.md), [Ray Data](docs/integrations/ray.md) |

The [full integration index](docs/integrations/README.md) also covers notebooks,
machine learning, visualization and custom workflows. The
[ecosystem comparison](docs/data-engineering-adbc-support.md) distinguishes
native connectors, custom adapters, local validation and unverified deployment paths.
A guide's availability does not imply upstream endorsement or hosted-service support.

For Flight SQL or JDBC clients, deploy the separate
[Redis Arrow Flight server](https://github.com/redis/redis-apache-arrow-flight),
which uses this driver as its SQL/Arrow backend. Check that project's client
examples and compatibility matrix for supported connections.

## Compatibility and validation

The driver requires **Redis Search (Query Engine)** and **RESP3**. Development and
tests use Redis Software with active, verified **4× QPF**. QPF is a server setting,
separate from driver worker settings; it does not promise a 4× speedup.

Recorded validation covers Redis Software, Redis Open Source (standalone and
cluster), and Redis Cloud, including Cloud Pro. See the
[compatibility report](docs/compatibility.md) for exact versions, dates, settings
and limitations, and [server requirements](docs/driver-reference.md#server-requirements)
for deployment and ACL requirements. Historical runs are evidence for their
recorded configurations.

Administrator-configured [SQL SELECT grants](docs/sql-rbac.md) support a restricted
Redis Software role/ACL mapping. Other privilege forms are rejected.

### Experimental Redis Flex stopgap

The opt-in `adbc.redis.flex=true` path supports plain indexes and bounded reads
on tested Software Flex builds. SQL restrictions remain and Cloud SQL is
unvalidated. See [scope, usage and evidence](docs/redis-flex-stopgap.md) before
using it; historical Cloud provisioning results are recorded separately in the
[Flex compatibility report](docs/redis-flex-compatibility.md).

## Apache Arrow feature matrix

The [detailed Arrow and ADBC matrix](docs/feature-matrices.md#apache-arrow-feature-matrix)
covers types, ingestion, IPC, streaming, partitions, cancellation and limits.
Supported interchange includes normalization; it does not guarantee preservation
of every physical input encoding or extension identity.

See [Arrow interoperability](docs/arrow-interoperability.md) for precise contracts,
[Substrait](docs/substrait.md) for the bounded binary plan subset, and
[experimental CUDA execution and Arrow C Device staging](docs/gpu-execution.md)
for opt-in GPU paths. CPU execution is the default.

## Redis Software feature matrix

The [detailed Redis Software matrix](docs/feature-matrices.md#redis-software-feature-matrix)
separates driver capabilities from server administration and companion services.
It includes authentication, TLS, cluster connectivity, recovery and deployment limits.

## Usage analytics

[Product usage analytics](analytics/README.md) reports high-level statement types,
execution counts, timings and row counts, plus documented source and deployment
metadata. **SQL statements, query parameters, query results and database contents
are not collected.** Reporting is on by default and uses
`https://adbc-telemetry.sa.redislabs.com/v1/usage`.

Opt out with the database option `adbc.redis.telemetry.enabled=false`.

## Development

The Go implementation builds on [driverbase-go](https://github.com/adbc-drivers/driverbase-go)
and uses the [ADBC validation suite](https://github.com/adbc-drivers/validation).
Start with [Building from source](docs/building.md), [CLAUDE.md](CLAUDE.md),
and the [development workflow](docs/sdlc/README.md).

For the complete default Redis Software test suite:

```bash
make -C go test-software
```

This target verifies effective 4× QPF and `noeviction` through the management API
before running tests. Tests modify data; use a dedicated fixture. Concurrent
sessions need separate Compose project names and API/database ports.

For native distribution and registry packaging, see the
[managed installer](docs/native-installation.md) and [dbc guide](docs/dbc.md).
