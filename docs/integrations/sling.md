# Move SQL data into Redis HASHes and export Redis data with Sling

[Sling](https://github.com/slingdata-io/sling-cli) can move PostgreSQL data into
Redis through this driver's native ADBC library and read it back into PostgreSQL.
Tested on macOS ARM64 with Sling 1.6.6, PostgreSQL 18.6, and Redis Software
8.8.0 with three shards and verified active 4x QPF. This is a tested configuration,
not a claim that every Sling load mode works.

## Why Sling is useful for Redis data movement

[Sling](https://slingdata.io/) is a CLI for extracting and loading tabular data.
Its broad adapter catalog lets you reuse connection definitions and transfer
commands across databases, warehouses and storage systems. For example:

| Connection family | Examples in Sling's catalog |
| --- | --- |
| SQL databases | PostgreSQL, MySQL, MariaDB, SQL Server, Oracle, SQLite, DuckDB |
| Analytics and warehouses | Snowflake, BigQuery, Redshift, Databricks, ClickHouse, Trino |
| Object storage | Amazon S3, Google Cloud Storage, Azure Storage, Cloudflare R2, MinIO |
| Files and transfer services | Local files, SFTP, FTP, Google Drive |

These are examples from [Sling's connection catalog](https://docs.slingdata.io/connections),
checked on 2026-11-10. Each adapter has its own supported directions, types,
authentication and load modes. A catalog entry does not establish that every
Redis-to-connector combination has been tested.

The Redis ADBC driver supplies the Redis side of the pipeline:

```text
PostgreSQL / MySQL -> Sling -> Redis ADBC -> managed Redis HASH rows + Search
Redis managed table -> Redis ADBC -> Sling -> SQL database / warehouse / file
```

Sling handles the source or destination adapter and transfer orchestration. The
driver exposes Redis tables through SQL and exchanges Arrow batches with Sling.
The other system can use its ordinary Sling connector; it does not need an ADBC
driver. This configured Redis integration is separate from Sling's built-in
connector catalog.

### Migrate data from PostgreSQL or MySQL

Sling plus Redis ADBC is useful for migrating SQL datasets into Redis while
retaining a SQL query interface over the imported data. The driver's recorded
PostgreSQL comparison result is **602/651 scored cases passing (92.47%)**, with
49 failures, two allowed extensions and two explicit exclusions, on Redis
Software with verified active 4x QPF. This is a result for the curated comparison
corpus, not a percentage of the entire PostgreSQL SQL language. See the
[differential harness](../../go/pgdiff/README.md#postgresql-18-final-query-gap-follow-up-2026-11-10)
and [PostgreSQL compatibility scope](../postgresql18-compatibility.md).

That measured query coverage makes Redis ADBC worth evaluating for datasets and
queries currently in PostgreSQL. MySQL data can use the same loading approach
through Sling's MySQL adapter, with destination types and queries mapped to the
driver's SQL dialect. The PostgreSQL comparison does not measure MySQL dialect
compatibility. Validate your application's queries, constraints, precision,
time zones and NULL behavior before switching it to the Redis copy.

This is a data migration workflow. It does not migrate stored procedures,
triggers, grants or complete database behavior. The driver uses autocommit and
does not provide multi-statement transaction rollback. The recorded Sling test
below verifies PostgreSQL value round trips; MySQL migration is an adaptation
example that has not been run by that test.

### What “Redis HASHes” means here

A destination such as `demo.inbound` is a driver-managed SQL table whose rows
are stored in Redis HASHes, with driver metadata, private physical keys and
Search indexes. A SQL `id` column is data; it does not instruct Sling to generate
an application key such as `customer:42`. Use the driver to query and modify its
managed tables; direct HSET writes to their private keys bypass the storage
protocol. See the [current snapshot contract](../snapshots.md).

Application-owned HASHes already in Redis first need an explicit import into a
managed table. A later section shows that route. Importing creates a copy;
subsequent source changes require an explicit refresh.

## End-to-end setup

The commands below use Bash on macOS or Linux, a local Sling process and a
dedicated Redis Software development database. Install Docker with Compose,
the Go toolchain declared in `go/go.mod`, a C compiler for cgo, Python 3, curl,
jq and OpenSSL. Run repository commands from the repository root.
Use a disposable PostgreSQL database or a development database you own.

### 1. Install Sling and build the native Redis driver

Pin Sling to the version with recorded integration evidence:

```sh
curl -fsSL https://slingdata.io/install.sh | bash -s -- v1.6.6
export PATH="$HOME/.sling/bin:$PATH"
sling --version

make -C go redis-up
make -C go build
```

The [official Sling installer](https://docs.slingdata.io/sling-cli/getting-started)
accepts a version argument. `redis-up` starts or reuses Redis Software and
verifies effective active QPF=4, six Search workers, mnp routing/32 connections
and noeviction before completing. Do not continue after a failed verification.
It writes private connection settings to ignored `go/build/test.env`.

The build creates `go/build/libadbc_driver_redis.dylib` on macOS or
`go/build/libadbc_driver_redis.so` on Linux. Build and run Sling on matching
architectures. These are the Redis database driver libraries; Sling also needs
an ADBC driver manager. Sling can download the manager on first use. Offline
installations can set `ADBC_DRIVER_MANAGER_LIB` to an installed manager's absolute
path; see [Sling's ADBC setup](https://docs.slingdata.io/connections/database-connections/adbc).

### 2. Create an isolated Sling home and install the SQL template

```sh
umask 077
export SLING_HOME_DIR="$PWD/go/build/sling-guide"
export SLING_DISABLE_TELEMETRY=true
mkdir -p "$SLING_HOME_DIR/templates"
cp scripts/sling/adbc.yaml "$SLING_HOME_DIR/templates/adbc.yaml"

source go/build/test.env
export REDIS_URI SSL_CERT_FILE
case "$(uname -s)" in
  Darwin) export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" ;;
  Linux)  export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so" ;;
  *) echo 'This walkthrough requires macOS or Linux'; exit 1 ;;
esac
```

The [template](../../scripts/sling/adbc.yaml) supplies Redis-compatible schema
and column discovery, DROP and rename statements. The override affects all
generic ADBC connections in this Sling home, so keep it isolated. It is required
for the documented Sling 1.6.6 workflow; simply naming the Redis library does not
supply Sling's missing Redis SQL templates.

### 3. Configure the connections

Use the verified Redis setup's environment values to construct Sling's named
connection without putting credentials in the guide:

```sh
export REDIS_ADBC="$(python3 - <<'PYCONFIG'
import json
import os
print(json.dumps({
    "type": "adbc",
    "driver_name": "redis",
    "driver": os.environ["REDIS_ADBC_LIBRARY"],
    "uri": os.environ["REDIS_URI"],
    "adbc.redis.tls_ca_file": os.environ["SSL_CERT_FILE"],
    "adbc.redis.partition_control_persistence": "guaranteed",
    "insert_concurrency": 1,
}))
PYCONFIG
)"
```

The persistence declaration above follows the policy verification performed by
`redis-up`. For another endpoint, verify its persistence and eviction policy
before setting that declaration. Keep TLS certificate verification enabled.

Supply `PG_SLING` from your secret manager or private environment. Its value is a
PostgreSQL URI with this shape (replace every placeholder):

```sh
export PG_SLING='postgresql://USER:URL_ENCODED_PASSWORD@HOST:PORT/DATABASE?sslmode=verify-full'
sling conns test PG_SLING
sling conns test REDIS_ADBC
```

Configure the PostgreSQL CA trust required by your environment. Use URI encoding
for reserved characters in credentials. Connection values and local Sling logs
are private operational data; keep them outside committed files. Environment
connections avoid persisting credentials in a repository YAML file. Sling also
supports a private `env.yaml` under its home directory, as described in its
[environment documentation](https://docs.slingdata.io/sling-cli/environment).

## Write SQL data into Redis

### 4. Seed a small PostgreSQL source and precreate the Redis table

Use a fresh `demo` schema in both dedicated development databases. The following
commands create a three-row dataset; choose different names if these objects
already exist.

```sh
sling conns exec PG_SLING \
  'CREATE SCHEMA IF NOT EXISTS demo' \
  'CREATE TABLE demo.source (id BIGINT, label TEXT, active BOOLEAN, amount NUMERIC(18,2), day DATE, happened_at TIMESTAMP(6), ratio DOUBLE PRECISION)' \
  "INSERT INTO demo.source VALUES (1, 'Alice', true, 12.50, '2026-01-01', '2026-01-01 12:00:00.123456', 0.5), (2, '世界', false, -3.25, '2026-01-02', '2026-01-02 12:00:00.000001', 1.25), (3, NULL, NULL, NULL, NULL, NULL, NULL)"

sling conns exec REDIS_ADBC \
  'CREATE SCHEMA IF NOT EXISTS demo' \
  'CREATE TABLE demo.inbound (id BIGINT, label TEXT, active BOOLEAN, amount NUMERIC(18,2), day DATE, happened_at TIMESTAMP(6), ratio DOUBLE PRECISION)'
```

Precreation sets the destination's intended types and avoids the default staging
path's known empty-batch problem. For real migrations, map the source schema
explicitly before loading; check unsigned integers, decimal precision, timestamp
zones, binary columns and database-specific types.

### 5. Transfer PostgreSQL rows to Redis and read them back

**The next command deletes existing rows in `demo.inbound` before loading.**
A failed run can leave the table empty or partially loaded. Use a dedicated target.

```sh
sling run --src-conn PG_SLING --src-stream demo.source \
  --tgt-conn REDIS_ADBC --tgt-object demo.inbound \
  --mode truncate --tgt-options '{"direct_insert":true}'

sling conns exec REDIS_ADBC \
  'SELECT id, label, amount FROM demo.inbound ORDER BY id' \
  'SELECT COUNT(*) AS row_count FROM demo.inbound'
```

The count should be three; the third row's label and amount should be NULL.
`direct_insert` bypasses Sling's staging table, as documented in
[Sling's target options](https://docs.slingdata.io/concepts/replication/target-options).
Keep `insert_concurrency: 1` on the Redis connection for this verified approach.
Truncate plus loading spans multiple statements and is not an atomic replacement,
even though the driver provides statement snapshots.

### Adapt the input to MySQL

Sling's [MySQL connector](https://docs.slingdata.io/connections/database-connections/mysql)
can supply the source instead. Provide a private `MYSQL_SOURCE` connection, for
example with this JSON shape and the TLS settings required by your deployment:

```json
{
  "type": "mysql",
  "host": "MYSQL_HOST",
  "port": 3306,
  "user": "MIGRATION_USER",
  "password": "SECRET_FROM_YOUR_ENVIRONMENT",
  "database": "sales"
}
```

Export that JSON as `MYSQL_SOURCE`, precreate `demo.mysql_customers` through
`REDIS_ADBC` with SQL types matching the source's selected columns, then run:

```sh
sling conns test MYSQL_SOURCE
sling run --src-conn MYSQL_SOURCE --src-stream sales.customers \
  --tgt-conn REDIS_ADBC --tgt-object demo.mysql_customers \
  --mode truncate --tgt-options '{"direct_insert":true}'
```

This is a migration recipe, not a recorded MySQL-to-Redis test result. Source SQL
runs against MySQL; SQL used to query the destination must follow the Redis
driver's supported dialect. Sling loading does not translate an application's
MySQL-specific queries or implement ongoing change-data capture.

## Read Redis data into another system

### 6. Export the Redis table to PostgreSQL

This command replaces the development destination `demo.roundtrip`:

```sh
sling run --src-conn REDIS_ADBC --src-stream demo.inbound \
  --tgt-conn PG_SLING --tgt-object demo.roundtrip --mode full-refresh

sling conns exec PG_SLING \
  'SELECT * FROM demo.roundtrip ORDER BY id' \
  'SELECT COUNT(*) AS row_count FROM demo.roundtrip' \
  'SELECT COUNT(*) AS differing_rows FROM ((SELECT * FROM demo.source EXCEPT ALL SELECT * FROM demo.roundtrip) UNION ALL (SELECT * FROM demo.roundtrip EXCEPT ALL SELECT * FROM demo.source)) AS differences'
```

Expect three destination rows and zero differing rows. `EXCEPT ALL` checks values
and duplicate multiplicity in both directions; row counts alone cannot establish
that a migration preserved data. For production datasets, compare all relevant
columns and types, plus application queries, before cutover.

### 7. Export to local files or a warehouse adapter

Sling's [local file connector](https://docs.slingdata.io/connections/file-connections/local)
accepts a file URI as the output object:

```sh
sling run --src-conn REDIS_ADBC --src-stream demo.inbound \
  --tgt-object "file://$SLING_HOME_DIR/inbound.csv"
```

For a warehouse, supply its named Sling connection and object name. For example,
export a private `SNOWFLAKE_TARGET` connection with your actual account, user,
database, schema, warehouse and approved authentication settings. Sling documents
these in its [Snowflake connector guide](https://docs.slingdata.io/connections/database-connections/snowflake).
Then, for a disposable destination table:

```sh
sling conns test SNOWFLAKE_TARGET
sling run --src-conn REDIS_ADBC --src-stream demo.inbound \
  --tgt-conn SNOWFLAKE_TARGET --tgt-object DEMO.REDIS_CUSTOMERS \
  --mode full-refresh
```

The file and Snowflake commands illustrate adapter reuse; they were not part of
the recorded PostgreSQL round-trip validation. Warehouse permissions, type
mapping and staging requirements remain specific to each connector. Choose a
target adapter with write support and verify the resulting data independently.

## Export existing application HASHes

If an application already owns HASHes such as `customer:1` and `customer:2`,
Sling cannot discover them as SQL tables merely from the Redis URI. Import the
collection first using the repository's HASH CLI. This example assumes that
collection already exists in the same dedicated Redis database and that `demo`
has been created above:

```sh
make -C go adbc-cli

go/build/redis-adbc check \
  -option "adbc.redis.tls_ca_file=$SSL_CERT_FILE" \
  -option adbc.redis.partition_control_persistence=guaranteed \
  -prefix customer: -schema demo -table application_customers

go/build/redis-adbc copy \
  -option "adbc.redis.tls_ca_file=$SSL_CERT_FILE" \
  -option adbc.redis.partition_control_persistence=guaranteed \
  -prefix customer: -schema demo -table application_customers \
  -sample -1 -key-column source_key -mode create

sling run --src-conn REDIS_ADBC --src-stream demo.application_customers \
  --tgt-conn PG_SLING --tgt-object demo.application_customers \
  --mode full-refresh
```

The CLI reads `REDIS_URI` from the environment. Inspect `check`'s inferred columns
before copying; add `-type field=SQL_TYPE` flags when necessary. `-sample -1`
considers all source HASHes for schema inference, and `source_key` retains the
original Redis key as an ordinary exported column. The source collection remains
application-owned and unchanged. Reserve memory for the managed copy. Its capture
is not atomic with concurrent application writes; coordinate writers if the
migration needs a consistent capture.

To refresh the copy, rerun the same `copy` command with `-mode replace`, then
rerun the Sling export. This is an explicit refresh, not a live link or CDC.
See [HASH collection import](../driver-reference.md#existing-hash-collections-redis-adbc)
and the [current import ownership rules](../snapshots.md).

## Save repeatable transfers as YAML

After creating both schemas and the Redis table, save this as a private
`postgres-to-redis.yaml`:

```yaml
source: PG_SLING
target: REDIS_ADBC
defaults:
  mode: truncate
  target_options:
    direct_insert: true
streams:
  demo.source:
    object: demo.inbound
```

For the reverse transfer, use `redis-to-postgres.yaml`:

```yaml
source: REDIS_ADBC
target: PG_SLING
defaults:
  mode: full-refresh
streams:
  demo.inbound:
    object: demo.roundtrip
```

Run them with the same environment connections and isolated Sling home:

```sh
sling run -r /private/path/postgres-to-redis.yaml
sling run -r /private/path/redis-to-postgres.yaml
```

The files use [Sling's replication format](https://docs.slingdata.io/concepts/replication).
They contain connection names rather than credentials. The same destructive
truncate/full-refresh semantics apply. These YAML equivalents have been checked
against the documented format; the recorded integration harness uses CLI flags.

## Clean up the walkthrough

After inspecting the results, remove only the objects created for this walkthrough:

```sh
sling conns exec REDIS_ADBC 'DROP TABLE IF EXISTS demo.inbound'
sling conns exec PG_SLING \
  'DROP TABLE IF EXISTS demo.roundtrip' \
  'DROP TABLE IF EXISTS demo.source'
```

If you also ran the optional examples, separately remove their destination tables
and exported file after review. Do not delete the source application HASHes. The
shared Redis Software fixture remains running for other development work.

## Run the integration test

Install the official Sling 1.6.6 binary for your platform. Docker, a C compiler,
Go, Python 3, curl, jq, and OpenSSL must be available for the repository setup.
From the repository root:

```sh
python3 scripts/test_sling_postgres.py --sling /absolute/path/to/sling
```

The [test](../../scripts/test_sling_postgres.py) runs `make -C go redis-up` to
verify effective QPF=4, six Search workers, mnp routing/32 connections and
noeviction, then builds the shared library with `make -C go build`. It starts a
disposable PostgreSQL 18 container on a randomly assigned loopback port, seeds
10,003 rows, runs two complete round trips, and verifies both directions with
PostgreSQL `EXCEPT ALL`. It removes its uniquely named Redis schema and PostgreSQL
container/volume afterward. The shared Redis Software fixture stays running.
Raw logs, isolated Sling configuration, and local endpoints stay in ignored
`go/build/sling-*/` directories. Sling telemetry is disabled by the test.
Sling may download its ADBC driver manager into that isolated directory.

## Connection and load configuration

Use `type: adbc`, `driver_name: redis`, and an explicit absolute path to
`libadbc_driver_redis.dylib` (macOS) or `libadbc_driver_redis.so` (Linux).
The Redis URI uses verified TLS. For example, an environment connection value is:

```json
{
  "type": "adbc",
  "driver_name": "redis",
  "driver": "/absolute/path/to/libadbc_driver_redis.so",
  "uri": "rediss://default:PASSWORD@localhost:6379/0",
  "adbc.redis.tls_ca_file": "/absolute/path/to/ca.pem",
  "adbc.redis.partition_control_persistence": "guaranteed",
  "insert_concurrency": 1
}
```

Set the persistence declaration only after verifying the endpoint's policy;
the test performs that verification. Use a dedicated test database. Tables must
be created or imported through the driver; arbitrary Redis keys are not SQL tables.

Copy [the Redis ADBC template](../../scripts/sling/adbc.yaml) to
`$SLING_HOME_DIR/templates/adbc.yaml`. Use an isolated Sling home because this
override applies to all generic ADBC connections in that home. It supplies
schema/column discovery and idempotent DROP/rename SQL through Sling's supported
[template override mechanism](https://github.com/slingdata-io/sling-cli/blob/9d5186194e76e3e146b7d6367a6e27a961242dce/core/dbio/dbio_types.go).

Precreate the Redis schema and table through `sling conns exec REDIS_ADBC` with
the intended SQL types. Then run:

```sh
sling run --src-conn PG_SLING --src-stream demo.source \
  --tgt-conn REDIS_ADBC --tgt-object demo.inbound \
  --mode truncate --tgt-options '{"direct_insert":true}'

sling run --src-conn REDIS_ADBC --src-stream demo.inbound \
  --tgt-conn PG_SLING --tgt-object demo.roundtrip --mode full-refresh
```

Truncate/direct mode destroys the target's existing rows before loading and is
not an atomic replacement. Failure can leave an empty or partially loaded table.
No rollback or CDC/upsert guarantee is implied. The verified path uses Sling's
ordinary row conversion and Arrow bulk ingestion; the token-gated Arrow lane
was not enabled.

## Observed limitations

- Stock `driver_name: redis` full-refresh into Redis fails with
  `Could not find template schemas` before ingestion. Redis is not one of
  Sling's recognized driver names for SQL-template delegation.
- Selecting PostgreSQL templates while retaining the explicit Redis library
  fails on Sling's metadata query `(select current_catalog)`. That configuration
  was investigated but is not the tested solution.
- After adding metadata templates, default staging encounters a Sling panic
  when an absent table's column query returns an empty Arrow batch. The smaller
  reproducer is `sling conns exec REDIS_ADBC 'SELECT label FROM demo.inbound WHERE 1=0'`
  against an existing table. Sling 1.6.6 exits nonzero with an Arrow string
  array bounds panic. Its `makeRecordNextFunc` tries row zero without skipping
  zero-row batches. Empty record batches are valid Arrow; no driver change was
  made to hide this consumer behavior. Precreation/direct mode avoids this
  path for the nonempty transfers tested here. Empty-source transfers remain
  unvalidated and should not be assumed to work.
- Destination DDL is not preserved exactly: Sling inferred `varchar(65500)`
  from `text` and unconstrained `numeric` from `numeric(18,2)` on the return trip.
  All tested values and NULLs survived exactly; this is value compatibility,
  not complete PostgreSQL schema compatibility.

See [validation evidence](../sdlc/changes/sling-postgres/validation.md).
The interoperability results and compatibility gaps were reported upstream in
[Sling issue #836](https://github.com/slingdata-io/sling-cli/issues/836).
