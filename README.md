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

## Contents

- [How tables and snapshots work](#how-tables-and-snapshots-work)
- [Quick start](#quick-start)
- [Live SQL and Redis access](#live-sql-and-redis-access)
- [Data tools and integration status](#data-tools-and-integration-status)
- [Compatibility and validation](#compatibility-and-validation)
- [Apache Arrow feature matrix](#apache-arrow-feature-matrix)
- [Redis Software feature matrix](#redis-software-feature-matrix)
- [Usage analytics](#usage-analytics)
- [Development](#development)

## How tables and snapshots work

The driver has **three table modes**. The main difference is who owns the HASHes
and who maintains the Search indexes.

| | Managed | Unmanaged | Live HASH tables |
|---|---|---|---|
| HASH owner | Driver | Application | Application |
| Index manager | Driver | Application | Driver |
| SQL reads and writes | Yes | Yes, through a registered application index | Yes, on the original HASHes |
| Direct Redis reads and writes | Physical storage is internal; direct writes unsupported | Yes | Yes |
| Read consistency | Statement snapshots | Live source values | Live source values |
| SQL write publication | Supported writes publish atomically | Source writes can partially succeed | Per-row writes; a statement can partially succeed |
| Copy of application data | Import creates a private copy | No copy | No copy |
| Arrow ingestion | Supported | Supported within the unmanaged write contract | Not supported initially |

**Choose managed tables** for SQL/Arrow pipelines and consistent analytical reads.
They are the default. SQL and Arrow writes go through the driver, which maintains
schema, indexes, snapshots, compaction, and reclamation. Importing application
HASHes creates a managed copy; later source changes require an explicit refresh.

**Choose unmanaged tables** when your application already owns a compatible Search
index and should keep responsibility for its configuration and lifecycle. Register
that index for SQL access to the original HASHes. See [unmanaged tables](docs/unmanaged-tables.md)
for registration, source writes, Arrow ingestion, and limits.

**Choose live HASH tables** when you want SQL and Redis clients to read and write
one set of application-owned HASHes, while the driver handles indexes. Register a
collection once; subsequent source changes need no import or refresh. Live mode
currently requires a source build. See the [example below](#live-sql-and-redis-access)
and the [live-mode contract](docs/live-hash-tables.md).

All modes use autocommit; multi-statement transactions and rollback are unsupported.
Unmanaged and live tables have no managed row snapshot: concurrent SQL and Redis
changes can interleave, and read-modify-write updates can lose intervening changes.
Direct Redis writes bypass SQL validation.

See [managed snapshots](docs/snapshots.md) for storage compatibility and fixed limits,
or the [ownership comparison](docs/managed-vs-unmanaged-tables.md) for more detail.

## Quick start

This walkthrough builds the current source, starts a disposable Redis Software
fixture, and queries sample managed tables. The source build also includes live
HASH mode and automatic insert keys.

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

When finished with the examples, including the optional live-mode example below:

```bash
make -C go redis-down
```

## Live SQL and Redis access

Use the source build and Redis fixture from the quick start, with the Python
virtual environment active and its connection environment loaded. If you already
stopped the fixture, run `make -C go redis-up` and source `go/build/test.env` again.
Install the Redis Python client:

```bash
python -m pip install redis
```

This example creates an application HASH, registers its collection, inserts a row
through SQL, and edits that same row through a Redis client. The driver creates
and maintains the Search index.

```python
import os
import subprocess
import redis
from adbc_driver_redis import dbapi

uri = os.environ["REDIS_URI"]
redis_options = {"decode_responses": True}
if uri.startswith("rediss://") and os.environ.get("SSL_CERT_FILE"):
    redis_options["ssl_ca_certs"] = os.environ["SSL_CERT_FILE"]
r = redis.Redis.from_url(uri, **redis_options)
r.hset("demo:users:seed", mapping={"name": "Ada", "age": 37})

# Register once; the application HASHes stay where they are.
subprocess.run(
    ["go/build/redis-adbc", "live", "register",
     "-prefix", "demo:users:", "-table", "users", "-apply"],
    check=True, stdout=subprocess.DEVNULL,
)

with dbapi.connect(uri, db_kwargs={"adbc.redis.live_tables": "true"}) as conn:
    with conn.cursor() as cur:
        cur.execute("""
            INSERT INTO redis_live.users (name, age)
            VALUES ('Grace', 40) RETURNING __key
        """)
        key = cur.fetch_arrow_table()["__key"][0].as_py()
        print(r.hgetall(key))          # Read the SQL-created HASH directly.
        r.hset(key, "age", 41)         # Edit it with a Redis client.
        cur.execute("SELECT name, age FROM redis_live.users WHERE __key = ?", (key,))
        print(cur.fetch_arrow_table()) # SQL reads the updated value.
```

Omitting `__key`, or specifying `DEFAULT`, generates a key such as
`demo:users:<uuid>` using the SQL function `GEN_RANDOM_UUID()`. Use
`RETURNING __key` to retrieve it, or supply your own key. Existing keys are refused;
explicit NULL is rejected. The driver adds no internal row/version fields to the
application HASHes, and SQL UPDATE preserves TTLs and unrelated fields.

Live mode supports SELECT, INSERT, UPDATE, DELETE, and RETURNING. It initially
excludes Arrow ingestion, shared managed snapshots, retained execution, MERGE,
TRUNCATE, schema-changing DDL, and mixed managed/live write statements. Collection
queries scan keys in bounded batches and can materialize results; no speedup over
managed tables is promised. See [schema, consistency, and query limits](docs/live-hash-tables.md).

Remove the registration and its driver-owned index while keeping the source HASHes:

```bash
go/build/redis-adbc live unregister -table users -apply
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
project. Follow each guide for installation and compatibility limits; the core
driver package does not install those adapters. Superset 6.1 has a documented
[compatibility blocker](docs/integrations/superset.md).

The [integration index](docs/integrations/README.md) and
[ecosystem comparison](docs/data-engineering-adbc-support.md) distinguish native
connectors, custom adapters, local validation, and unverified deployment paths.
For Flight SQL or JDBC, see the separate
[Redis Arrow Flight server](https://github.com/redis/redis-apache-arrow-flight).

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
