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

# Arrow Database Connection (ADBC) driver for Redis

An [ADBC](https://arrow.apache.org/adbc/) driver built on
[driverbase-go](https://github.com/adbc-drivers/driverbase-go) and validated
with the [ADBC driver validation suite](https://github.com/adbc-drivers/validation).
It supports SQL queries and Arrow bulk ingestion.

## Contents

- [Compatibility and validation](#compatibility-and-validation)
- [Server requirements](#server-requirements)
  - [Prerequisites](#prerequisites)
  - [Driver-managed setup](#driver-managed-setup)
  - [Timeouts](#timeouts)
  - [URI parameters](#uri-parameters)
  - [Cancelling a statement](#cancelling-a-statement)
  - [Error handling](#error-handling)
  - [ACL users](#acl-users)
  - [Limitations](#limitations)
- [Quick start](#quick-start)
- [Existing HASH collections: `redis-adbc`](#existing-hash-collections-redis-adbc)
  - [Build and connect](#build-and-connect)
  - [Check collections](#check-collections)
  - [Column inference](#column-inference)
  - [Copy HASHes into a table](#copy-hashes-into-a-table)
  - [Adopt HASHes in place](#adopt-hashes-in-place)
  - [ACL permissions for `redis-adbc`](#acl-permissions-for-redis-adbc)
  - [Sweep leftover rows](#sweep-leftover-rows)
- [Architecture: hybrid index-row layout](#architecture-hybrid-index-row-layout)
  - [Rows and indexes](#rows-and-indexes)
  - [Metadata and key naming](#metadata-and-key-naming)
  - [Query execution](#query-execution)
  - [Streamed results](#streamed-results)
  - [Aggregate pushdown](#aggregate-pushdown)
- [Supported SQL](#supported-sql)
  - [Schema objects and DDL](#schema-objects-and-ddl)
  - [Inserts, parameters, defaults, and constraints](#inserts-parameters-defaults-and-constraints)
  - [Queries and relational features](#queries-and-relational-features)
  - [Expressions, aggregates, and functions](#expressions-aggregates-and-functions)
  - [Additional statements and metadata](#additional-statements-and-metadata)
  - [Transactions, limitations, and types](#transactions-limitations-and-types)
- [Options](#options)
- [Performance](#performance)
  - [Results](#results)
  - [Query performance factor (QPF)](#query-performance-factor-qpf)
  - [Reproduce the benchmark](#reproduce-the-benchmark)
- [Building and testing](#building-and-testing)
  - [Validation suite](#validation-suite)
  - [Go tests](#go-tests)
  - [Redis Cluster](#redis-cluster)
  - [Redis Flex](#redis-flex)
  - [Cross-platform builds and releases](#cross-platform-builds-and-releases)
  - [Python usage](#python-usage)

## Compatibility and validation

On 2026-10-06, the current validation suite against local Redis Open Source
8.6.2 had 360 passed, 8 skipped, 3 subtests passed, and 0 failed. The suite
has also been run against the servers below; the cluster and Redis Cloud
results are from earlier suite revisions and have not been re-run for this
update.

| Server | Version | Connection |
|-|-|-|
| Redis Open Source (`redis:8.6.2` Docker image) | 8.6.2, Search 8.6.0 | `redis://` |
| Redis Open Source cluster, 3 shards (OSS Cluster API) | 8.6.2, Search 8.6.0 | `redis://` |
| Redis Cloud, single shard | 8.6.2, Search 8.6.10 | `redis://` and TLS (`rediss://`)*† |
| Redis Cloud Pro, 2 shards, through the proxy endpoint | 8.6.2 | `redis://`† |
| Redis Cloud Pro, 2 shards, OSS Cluster API enabled | 8.6.2 | `redis://`† |

Local testing uses the Redis version that Redis Cloud runs (currently
8.6.2), so `go/compose.yaml` pins that image.

Earlier local and cluster runs had 325 passed, 0 failed. The Cloud runs
marked † had 322 passed, 0 failed, before the two temporary-table ingest
tests were enabled; they have not been re-run since. The TLS run marked \*
predates cluster support and has not been re-run since.

The 8 currently skipped tests cover features the driver doesn't offer:
constraints, statistics, a second catalog, and transactions. Of the
constraint tests, those for `CHECK` and `PRIMARY KEY` in `GetObjects` run
now; those for `UNIQUE` and `FOREIGN KEY` are still skipped.

Redis Flex (RAM + SSD) databases are not supported yet, and the driver
refuses them when it connects; see
[Server requirements](#server-requirements).

## Server requirements

You don't create any indexes or enable any settings yourself. The one hard
requirement is the Query Engine.

### Prerequisites

- **Redis with the Query Engine (RediSearch).** It is built into Redis 8.x
  (e.g. the `redis:8.6.2` image). On older versions use Redis Stack, or Redis
  Cloud / Redis Software with Search enabled. The driver runs `FT._LIST`
  when it connects and refuses to connect if search isn't available.
- **A standalone server, Redis Cloud / Redis Software, or a Redis Cluster.**
  The driver picks its client automatically (`adbc.redis.cluster=auto`):
  - If the server reports `cluster_enabled:1` (OSS Cluster API), it uses a
    cluster client. Rows spread across shards, and all search commands for a
    table go through one node, which coordinates the query across shards.
  - Otherwise it uses a single endpoint: standalone Redis, or Redis
    Cloud / Redis Software through their proxy.

  Driver metadata shares one hash slot (`adbc:{meta}:…`), so the driver's
  multi-key transactions never cross slots. A cluster only has database 0;
  on standalone servers only database 0 has been tested.
- **ACL permissions,** if the database has ACL users: the commands and keys
  under [ACL users](#acl-users) below.

### Driver-managed setup

- `CREATE TABLE` and bulk ingest create the table's index
  (`FT.CREATE idx:<schema>:<table> …`) and its metadata keys. `DROP TABLE`
  removes the index and all of the table's rows, and `TRUNCATE` moves the
  table to a new, empty index and key prefix and then deletes the old rows
  the same way (a statement still writing to the table then deletes what
  it writes; see "Key prefixes" in the architecture section).
- Every filterable column is indexed by default. Narrow this with `NOINDEX`
  in `CREATE TABLE` or `adbc.redis.ingest.index_columns` on ingest. Queries
  still work on columns that aren't indexed: the driver scans the table's
  rows and filters them itself, which is slower on large tables but correct.
- Every query passes `TIMEOUT 0`, so the server's default search timeout
  (which can return partial results) doesn't apply. You don't need to change
  any `search-*` settings or enable keyspace notifications. The client's own
  timeouts still apply; see [Timeouts](#timeouts) below.

### Timeouts

The driver's client waits up to 5 minutes for each reply from Redis, and
to send each command, on standalone servers and clusters alike. Some
commands take longer as the data grows: an `FT.AGGREGATE` page that sorts or
groups a big table, `FT.DROPINDEX … DD` of a big table, a pipeline of 1,000
rows. And a busy or briefly blocked server (a fork for a snapshot, a
failover, a slow cluster shard) delays every reply. When a reply takes
longer than the timeout, the statement fails with an error that names the
setting:

```text
[redis] FT.AGGREGATE failed: read tcp 127.0.0.1:57705->127.0.0.1:7103: i/o timeout (Redis did not reply within the read timeout, 5m0s: raise adbc.redis.read_timeout, or set it to 0 for no timeout)
```

| Setting | Default | What it limits |
|-|-|-|
| `adbc.redis.read_timeout` (database or connection option), or the URI's `read_timeout` | `5m` | waiting for each reply |
| `adbc.redis.write_timeout` (database or connection option), or the URI's `write_timeout` | the read timeout | sending each command |
| the URI's `dial_timeout` | `5s` | connecting |

- **Values** are durations such as `30s`, `10m` or `1h`, or a number of
  seconds. `0` means no timeout. The URI's parameters are parsed by go-redis
  (`redis://host:6379/0?read_timeout=30m`), where `0` and `-1` also mean no
  timeout.
- **Precedence:** the options override the URI's parameters. A connection's
  option overrides the database's, for the statements that the connection
  runs after it is set (the connection gets a new client). Connecting uses
  the database's.
- **Retries:** go-redis sends most commands again after a reply timed out
  or the connection broke (up to 3 times; see `max_retries` under **URI
  parameters**), so a statement can wait a few times the read timeout
  before it fails. The driver turns that off for the commands where a
  second run isn't safe: search commands (a repeated `FT.CURSOR READ` would
  return the next page, and the rows of the lost one would be missing), and
  `SET … NX` and `SADD` where the driver acts on the reply. Transactions
  (`MULTI` … `EXEC`) aren't sent again once they were sent.
- **Earlier versions** (v0.0.7 and before) used go-redis's defaults: 5
  seconds, in effect 10 seconds on a standalone server.

### URI parameters

go-redis parses the URI's query parameters
(`redis://host:6379/0?pool_size=20&client_name=etl`). The single-endpoint
client and the cluster client both get them. An unknown parameter is an
error.

- **Both clients:** `client_name`, `max_retries`, `min_retry_backoff`,
  `max_retry_backoff`, `dial_timeout`, `read_timeout`, `write_timeout`,
  `pool_fifo`, `pool_size`, `pool_timeout`, `min_idle_conns`,
  `max_idle_conns`, `max_active_conns`, `max_concurrent_dials`,
  `conn_max_idle_time`, `conn_max_lifetime`, `conn_max_lifetime_jitter`,
  `skip_verify` with `rediss://`, and `db` (as the path; a cluster only has
  database 0).
- **On a cluster,** every node's client gets them. `pool_size` and
  `max_active_conns` are per node. `max_retries` is per node too: a node's
  client sends a command again that many times (by default none), and the
  cluster client tries again up to `max_redirects` times (3).
- **Cluster only:** `addr` (another seed node, `host:port`; repeat it for
  more), `max_redirects`, `dialer_retries`, `dialer_retry_timeout` and
  `failing_timeout_seconds`. On a single endpoint they're an error. The
  seeds help when the URI's node is down: `adbc.redis.cluster=auto` asks
  that node what it is, so also set `adbc.redis.cluster=true`.
- **Not supported:** `read_only`, `route_by_latency` or `route_randomly`
  set to true is an error. They send reads to replicas, which may not have
  the driver's latest writes yet.
- **The driver's settings win:** the `username` and `password` options,
  no `CLIENT SETINFO`, and the timeout options (see [Timeouts](#timeouts)).
  The protocol defaults to RESP3; set `protocol=2` for RESP2 compatibility,
  or `protocol=3` explicitly. The setting reaches every cluster node too.
- **`client_name`** makes go-redis name every connection with `CLIENT
  SETNAME`. An ACL user then needs `+client|setname` (see
  [ACL users](#acl-users)).
- **Earlier versions** (v0.0.12 and before) gave the cluster client only
  the URI's credentials, TLS and timeouts, and refused the cluster-only
  parameters as unexpected.

### Cancelling a statement

Cancelling a statement while it executes (`AdbcStatementCancel`;
`cursor.adbc_cancel()` in Python, from another thread), or cancelling the
context passed to `ExecuteQuery` or `ExecuteUpdate` in Go, makes it fail
with CANCELLED:

```text
[redis] the query was cancelled
```

- **The work the driver does in memory** (filtering, joins, aggregates,
  window functions, sorting, set operations, recursive queries, subqueries,
  `generate_series`) checks for the cancel at every row, so it stops within
  milliseconds. A sort under way finishes first.
- **Redis commands:** the cancel stops the statement at its next command.
  One already sent waits for its reply (an `FT.AGGREGATE` page, a pipeline
  of 1,000 rows), up to the read timeout. The error then names the step:
  `[redis] failed to write rows: context canceled`.
- **Writes:** `INSERT`, `UPDATE`, `DELETE` and `MERGE` compute every change
  before they write one, so cancelled until then they change nothing.
  There are no transactions, so one cancelled while it writes keeps the
  rows it has written, as does `CREATE TABLE … AS` (whose table is created
  first). A rename with `adbc.redis.rename_rekey` is rolled back.
- **A Go context's deadline** fails the statement the same way, with
  TIMEOUT.
- **SQLSTATE** `57014` (query_canceled) in both cases, as Postgres reports a
  cancel and a statement timeout (see [Error handling](#error-handling)).
- **Earlier versions** (v0.0.11 and before) noticed a cancel only at the
  statement's next Redis command, which then failed with IO.

### Error handling

An error that Postgres has a SQLSTATE for carries the one Postgres 16
reports for the same statement: `adbc.Error.SqlState` in Go,
`AdbcError.sqlstate` in C, and `exc.sqlstate` in Python, whose
`adbc_driver_manager` also ends the message with `SQLSTATE: 22P02`. The
SQLSTATE sets the ADBC status, as it does in adbc-driver-postgresql, and so
the Python exception:

| Error | SQLSTATE | Status | Python |
|-|-|-|-|
| A value that doesn't convert or doesn't fit (`CAST('abc' AS INTEGER)`, `'2024-02-30'::date`, `1 / 0`, a value too long for its column) | class `22` | `InvalidData` | `DataError` |
| `NOT NULL` and `CHECK` | `23502`, `23514` | `Integrity` | `IntegrityError` |
| A table, view, column, function or schema that doesn't exist; an operator for a quoted literal that doesn't exist (`b + '1'`) | `42P01`, `42703`, `42883`, `3F000`; `42883` | `NotFound` | `ProgrammingError` |
| A table, view, column, constraint or schema that already exists; a column named twice (in `INSERT`, a `PRIMARY KEY`, a table or a view) | `42P07`, `42701`, `42710`, `42P06` | `AlreadyExists` | `ProgrammingError` |
| A syntax error; a column name that several columns have (`column reference "x" is ambiguous`); a table definition Postgres refuses (two primary keys, `DROP NOT NULL` of a key column, a column of type record); a type that never converts (`DATE` to `BOOLEAN`); a record where another type is needed (`argument of WHERE must be type boolean, not type record`); values without a common type (`CASE`, `COALESCE`, `UNION`, …; see "Quoted string literals"); an operator of a quoted literal that could be one of several (`d + '1'`, `'1' + '1'`) | `42601`; `42702`; `42P16`; `42846` in a cast, `42804` written to a column or by `ALTER COLUMN … TYPE`; `42804`; `42804`; `42725` | `InvalidArgument` | `ProgrammingError` |
| What Postgres doesn't support either (`DATE_BIN` by months, `EXTRACT(day FROM time)`, `ALTER COLUMN … TYPE` of a column a view reads) | `0A000` | `NotImplemented` | `NotSupportedError` |
| A cancel, or a Go deadline | `57014` | `Cancelled`, `Timeout` | `OperationalError` |

- **Data exceptions** have Postgres's code for each case: `22P02` for text
  that isn't a number, boolean, JSON or array; `22003` for a number out of
  its type's range (also an integer overflow, and NaN or infinity cast to
  `NUMERIC`, which in Postgres overflow a `NUMERIC(p, s)`); `22007` for text
  that isn't a date, time, time with time zone, timestamp or interval, or
  doesn't match a `TO_DATE` pattern; `22008` for date and time fields or
  results out of range (`'25:00'::time`, `MAKE_DATE(2024, 2, 30)`); `22009`
  for a time zone offset out of range (`'10:00+16'::timetz`); `22012` for
  division by zero; `22001` for a value too long; `22023` for an argument out of its
  domain (an unknown time zone or `EXTRACT` field, `SPLIT_PART` position 0,
  a zero `generate_series` step, `VARCHAR(0)`); `2201B` for a regular
  expression that doesn't compile; and the codes of logarithms (`2201E`),
  powers and square roots (`2201F`), substrings (`22011`), window function
  arguments (`22013`, `22014`, `22016`), NULL object keys (`22004`) and
  SQL/JSON paths (`2203A`, `22033`, `22039`).
- **Every path** gives them: queries (also while a streamed result is
  read), `INSERT`, `UPDATE`, `MERGE`, `ALTER COLUMN … TYPE`, `DEFAULT`s,
  bound parameters and bulk ingest. A value written to a column has the
  column's name in front: `[redis] column "n": invalid input syntax for type
  integer: "x"`. `TRY_CAST` still returns NULL for a value that doesn't
  convert.
- **Messages** are mostly the driver's: only the SQLSTATE is Postgres's
  (`'abc'::numeric` is `invalid decimal "abc"`, where Postgres says
  `invalid input syntax for type numeric: "abc"`). Text read as an integer
  has Postgres's messages too (see Text as an integer under Casts), as do
  operators of quoted literals (see Quoted string literals).
- **No SQLSTATE yet:** the errors of the driver's own limits, and a few
  others: `more than one row returned by a subquery used as an expression`
  (`21000` in Postgres), an operator that doesn't exist (`42883`; those of
  rows, records and quoted literals have it), `missing
  FROM-clause entry for table "t"` (`42P01`), and a `SET` of an invalid
  value (`22023`). They keep their status, mostly `InvalidArgument`.
- **In Go,** `adbc.Error.Error()` ends with the SQLSTATE in parentheses:
  `Invalid Data: [redis] division by zero (22012)`.
- **Upgrading from v0.0.12 and earlier:** errors had no SQLSTATE, and a
  value that didn't convert failed with `InvalidArgument`
  (`ProgrammingError` in Python), unlike Postgres; only a value too long was
  `InvalidData`. An unknown column or function was `InvalidArgument`, as
  was a column named twice in `INSERT`; they are `NotFound` and
  `AlreadyExists` now. `DATE_BIN` by months and `EXTRACT` of a field the
  type doesn't have were `InvalidArgument`.

### ACL users

If the database has ACL users, the driver's user needs these commands. The
lists were checked with users that have exactly these rules, running the Go
integration tests and the ADBC validation suite on Redis 8.6.2, standalone
and a 3-shard cluster:

```text
# Every connection: queries, DDL, DML, views, bulk ingest, temporary tables
+ping +info +ft._list +ft.create +ft.alter +ft.info +ft.dropindex +ft.aggregate +ft.cursor +ft.search +ft.profile +get +set +del +exists +incrby +hset +hget +hmget +hgetall +hdel +hincrby +hexists +sadd +srem +smembers +sismember +multi +exec +watch +unwatch
# A cluster (OSS Cluster API) also: the client reads the slot map and the commands' key positions
+cluster|slots +command
# adbc.redis.rename_rekey also
+incr +dump +restore
```

- **Keys:** `~adbc:*` (the driver's metadata), `~<schema>:*` for each schema
  the user works in (`~public:*` by default), and `~pg_temp_*` for temporary
  tables. Temporary tables and bulk ingest need no other commands.
- **What they're for:** `PING` and `INFO` to connect (`INFO` also tells a
  cluster apart, and gives the server version); the `FT.*` commands for the
  tables' indexes and queries (`FT.PROFILE` checks that a `LIKE 'abc%'`
  filter's prefix expansion was complete: without it such filters are
  still right, but are evaluated by the driver; `FT.INFO` lists an index's
  attributes for `ALTER TABLE … ADD COLUMN`); the string, hash and set
  commands for metadata and rows (`HGETALL` reads pending re-keying
  renames on every connect, and `HEXISTS` checks that a new table's key
  prefix was never released); and `MULTI` / `EXEC` / `WATCH` / `UNWATCH`
  for metadata transactions. `ALTER COLUMN … TYPE`, which rewrites the
  rows to new keys, needs only these (it copies rows with `HGETALL` and
  `HSET`, not `DUMP` / `RESTORE`).
- **Not needed:** RESP3 negotiation uses `HELLO`, but `HELLO` and `AUTH` are
  always allowed. Add `+select` if the URI names a database other than 0 (not
  tested), and `+client|setname` if it sets `client_name`. A `protocol=2` URI
  remains available for compatibility with RESP2-only deployments.
- **On a cluster,** ACL users are per node: create the user on every node.
- **For example,** for dbt on a cluster, with `rename_rekey`:

  ```text
  ACL SETUSER dbt on >password resetchannels ~adbc:* ~public:* ~pg_temp_* +ping +info +ft._list +ft.create +ft.alter +ft.info +ft.dropindex +ft.aggregate +ft.cursor +ft.search +ft.profile +get +set +del +exists +incrby +hset +hget +hmget +hgetall +hdel +hincrby +hexists +sadd +srem +smembers +sismember +multi +exec +watch +unwatch +cluster|slots +command +incr +dump +restore
  ```

A **read-only user** needs these commands, and read access to the keys:

```text
+ping +info +ft._list +ft.aggregate +ft.cursor +ft.search +ft.profile +get +exists +hget +hmget +hgetall +smembers +sismember
```

```text
ACL SETUSER reader on >password resetchannels %R~adbc:* %R~public:* +ping +info +ft._list +ft.aggregate +ft.cursor +ft.search +ft.profile +get +exists +hget +hmget +hgetall +smembers +sismember
```

(On a cluster, also `+cluster|slots +command`.)

- **What it can do:** queries, views, `information_schema`, and the ADBC
  metadata calls (`GetObjects`, `GetTableSchema`): what dbt's tests and
  `dbt docs generate` run (this was tested with the driver, not with dbt).
- **What it can't:** every change is refused before anything changes, with
  Redis's reason, for example `failed to allocate row ids: NOPERM User reader
  has no permissions to run the 'multi' command` for an `INSERT`. That
  includes temporary tables (allocating a temporary schema needs `INCRBY`),
  so dbt unit tests and snapshots need a user that can write.
- **Connecting needs no writes,** with two exceptions. The first connection
  to a new database records the table registry, so a read-only user can't
  be the first to connect (`failed to build the table registry: NOPERM …`).
  And a connection that finds a re-keying rename or a temporary schema that
  another connection abandoned cleans it up: a read-only user can't, and
  leaves that to the next connection that can write. (Redis's `ACL LOG`
  then shows the refused commands.)

#### Search commands and key patterns

RediSearch commands that change indexes declare no keys, so key patterns
don't apply to them:

- **`FT.DROPINDEX … DD`** deletes every row of a table, even for a user that
  may only read the table's keys. `FT.CREATE`, `FT.ALTER` and the
  `FT.ALIAS*` commands can change what the driver's indexes cover. So a user
  that mustn't change data must not get `FT.CREATE`, `FT.ALTER`,
  `FT.DROPINDEX` or `FT.ALIAS*` (the read-only rules above leave them out).
- **Queries** do check keys in Redis 8.6: `FT.SEARCH` and `FT.AGGREGATE` on
  an index whose key prefix the user can't read fail with `NOPERM User does
  not have the required permissions to query the index`, so per-schema key
  patterns keep a user out of other schemas' tables. `FT._LIST` still lists
  every index, so such a user sees the other schemas' table names. Check
  this on other Redis versions.

#### Permission errors

A command the user may not run fails with Redis's `NOPERM` error. One that
Redis refuses inside a transaction is reported with the
command and its key, rather than as `EXECABORT Transaction discarded because
of previous errors`: `failed to create view: NOPERM No permissions to access
a key ('set' on adbc:{meta}:view:public:v, in a MULTI transaction that Redis
discarded)`.

### Limitations

#### Existing HASH collections need `redis-adbc`

A table exists for the driver only if its metadata key
(`adbc:{meta}:table:<schema>:<table>`) exists. The metadata names the
table's key prefix and index. HASHes and indexes created some other way
(for example your own `row:*` / `idx:rows` layout) don't appear as tables
until `redis-adbc` makes them one: `copy` bulk-ingests them into a new
table, and `adopt` makes the HASHes themselves a table, so that SQL reads
what the application writes (see
[Existing HASH collections](#existing-hash-collections-redis-adbc)).
Adopting writes nothing into the HASHes, whatever their keys
(`user:42`, `user:alice`), and HASHes the application writes later are rows
too. It reads the rows through the application's own search index when
that can serve, and otherwise creates a second one, and an adopted
table refuses `INSERT` and `TRUNCATE`. The driver doesn't touch
indexes it didn't create, even one with the name a new table's index would
take (see "Indexes the driver didn't create are left alone" below).

#### Redis Flex databases are not supported yet

[Redis Flex](https://redis.io/docs/latest/operate/rs/flex/) (formerly Auto
Tiering) keeps most of a database on SSD, for datasets of a terabyte and
more. Search on Flex can't index tables the way the driver needs. Every
table's index has a `NUMERIC SORTABLE` row-ID field, and every query reads
rows through `FT.AGGREGATE` cursors. A Flex database with Search enabled
fails with `Redis Flex databases are not supported`, before writing anything.
A database without Search fails earlier with an error that the Redis server
does not provide the search (RediSearch) module.

| Flex database | Search on Flex | Missing for the driver |
|-|-|-|
| Redis Cloud Essentials | Not available | Search itself (the driver reports that search is missing) |
| Redis Cloud Pro without Search enabled, Redis 8.6 | Unavailable on the newly created test database | `FT._LIST`, `FT.CREATE`, `FT.SEARCH`, and `FT.AGGREGATE` were not registered. The driver reports that search is missing. |
| Redis Cloud Pro Search on Flex beta | Opt-in beta; not exercised in the live Cloud tests | `FT.AGGREGATE` and `NUMERIC` fields (per Redis's docs) |
| Redis Software 8.2.0-78, Redis 8.6.2, Search 8.6.11 | Included (`search_on_bigstore`) | `FT.AGGREGATE`, `FT.ALTER`, `FT.DROPINDEX … DD`; `NUMERIC`, `GEO` and `SORTABLE` fields. `FT.SEARCH` returns only keys, and indexes need `SKIPINITIALSCAN` (tested, see [Building and testing](#building-and-testing)) |

Redis's announcement of Search on Flex says Redis Software already offers
`NUMERIC` fields and `FT.AGGREGATE` on Flex, and that Redis Cloud will follow.
But the latest Docker image (8.2.0-78, built 2026-09-01) rejects both. The
connect check tests whether `FT.AGGREGATE` works, not whether the database
is Flex. So a Flex release that supports it is no longer refused.

The local and Cloud tests ran on 2026-10-06 against commit `40da0bf`.
See the [compatibility report](docs/redis-flex-compatibility.md) or its
[Word version](docs/redis-flex-compatibility.docx) for the settings, live
command results, and missing driver and server functions.

## Quick start

Requirements: Go 1.26.6 or newer, a C toolchain (cgo), Docker, and
[uv](https://docs.astral.sh/uv/). Run everything from the `go` directory.

Each [GitHub release](https://github.com/alberttwong/redis-adbc-driver/releases)
also has prebuilt libraries for macOS (Apple Silicon) and Linux (x86-64 and
arm64). The Linux libraries run on every Linux that
[Redis Software supports](https://redis.io/docs/latest/operate/rs/references/supported-platforms/):
RHEL 8 and 9 (and compatible distributions), Ubuntu 20.04 and later, and
Amazon Linux 2023. To use one instead of building, put it at the path
`make build` would create (`go/build/libadbc_driver_redis.dylib` or `.so`).
Releases after v0.0.10 also have the
[`redis-adbc`](#existing-hash-collections-redis-adbc) command for the same
platforms.

### 1. Start Redis 8.6 and build the driver

```bash
cd go
docker compose up --detach --wait redis
make build
```

### 2. Load the sample data set

```bash
uv run --project validation python examples/load_sample.py
```

This creates two tables in the default `public` schema:

- `customers` (50 rows): created with `CREATE TABLE` / `INSERT`. `email` is
  declared `NOINDEX`, so it is stored in the row HASHes but not in the index.
- `sales` (10,000 rows, `--rows N` to change): bulk-ingested from an Arrow
  table with `order_id, customer_id, country, product, quantity,
  unit_price NUMERIC(10,2), discount, ordered_at TIMESTAMP WITH TIME ZONE,
  status, notes`. Every column except `notes` is indexed.

### 3. Run SQL

`examples/sql.py` runs one statement passed as an argument, or starts an
interactive prompt (statements end with `;`) when given none:

```bash
uv run --project validation python examples/sql.py "SELECT COUNT(*) FROM sales"
```

```bash
uv run --project validation python examples/sql.py
```

Common queries to try:

```sql
-- Browse rows (insertion order unless ORDER BY is given)
SELECT * FROM sales ORDER BY order_id LIMIT 5;

-- Count rows: GROUPBY 0 REDUCE COUNT inside the index
SELECT COUNT(*) FROM sales;

-- Filter on a TAG and a NUMERIC range, sort and limit inside the index,
-- then fetch just those 5 rows from their HASHes
SELECT order_id, country, product, quantity, unit_price
FROM sales
WHERE country = 'JPN' AND quantity >= 18
ORDER BY unit_price DESC
LIMIT 5;

-- Aggregate per group, computed from SORTABLE fields without opening HASHes
SELECT country, COUNT(*) AS orders, SUM(quantity) AS units, AVG(quantity) AS avg_units
FROM sales
GROUP BY country
ORDER BY units DESC;

-- Aggregate over an expression (computed exactly by the driver)
SELECT product, SUM(quantity * unit_price) AS revenue
FROM sales
WHERE status = 'shipped'
GROUP BY product
ORDER BY revenue DESC;

-- Timestamp range
SELECT order_id, ordered_at, status
FROM sales
WHERE ordered_at >= TIMESTAMP '2025-12-31 00:00:00'
ORDER BY ordered_at
LIMIT 5;

-- NULL handling
SELECT COUNT(*) AS no_discount FROM sales WHERE discount IS NULL;
SELECT order_id, notes FROM sales WHERE notes IS NOT NULL ORDER BY order_id LIMIT 3;

-- Full-row lookup by row id: a direct HMGET, the index is not used
SELECT __rowid, order_id, country, notes FROM sales WHERE __rowid = 42;

-- CASE expressions, also usable in GROUP BY (by alias or position)
SELECT order_id, quantity,
       CASE WHEN quantity >= 15 THEN 'bulk' WHEN quantity >= 5 THEN 'standard' ELSE 'small' END AS size
FROM sales ORDER BY order_id LIMIT 5;
SELECT CASE WHEN discount IS NULL THEN 'full price' ELSE 'discounted' END AS pricing, COUNT(*) AS orders
FROM sales GROUP BY pricing ORDER BY pricing;

-- HAVING filters groups; here the GROUPBY/REDUCE itself still runs in the index
SELECT country, COUNT(*) AS orders, SUM(quantity) AS units
FROM sales GROUP BY country HAVING SUM(quantity) > 17500 ORDER BY units DESC;

-- CREATE TABLE AS SELECT: column names and types come from the query
CREATE TABLE country_revenue AS
SELECT country, COUNT(*) AS orders, SUM(quantity * unit_price) AS revenue
FROM sales WHERE status = 'shipped' GROUP BY country;
SELECT * FROM country_revenue ORDER BY revenue DESC LIMIT 3;

-- Subqueries. An uncorrelated one runs once; its result is pushed into the
-- outer query's index filter (a range here, a union query for IN)
SELECT COUNT(*) AS above_avg FROM sales WHERE unit_price > (SELECT AVG(unit_price) FROM sales);
SELECT name, country FROM customers
WHERE customer_id IN (SELECT customer_id FROM sales WHERE quantity = 20 AND status = 'returned')
ORDER BY name LIMIT 5;

-- Correlated subqueries run per outer row (memoised on the outer values)
SELECT c.name, (SELECT COUNT(*) FROM sales s WHERE s.customer_id = c.customer_id) AS orders
FROM customers c ORDER BY orders DESC, c.name LIMIT 3;

-- EXISTS / IN correlated only by equalities can run once instead, as a hash
-- semi-join (here the matching customer ids also filter sales in the index)
SELECT COUNT(*) FROM sales s
WHERE EXISTS (SELECT 1 FROM customers c WHERE c.customer_id = s.customer_id AND c.country = 'JPN');

-- CTEs and derived tables are computed once and held in memory
WITH per_customer AS (
  SELECT customer_id, SUM(quantity) AS units FROM sales GROUP BY customer_id
)
SELECT customer_id, units FROM per_customer
WHERE units > (SELECT AVG(units) FROM per_customer)
ORDER BY units DESC LIMIT 3;
SELECT bucket, COUNT(*) AS orders
FROM (SELECT CASE WHEN quantity >= 15 THEN 'bulk' ELSE 'regular' END AS bucket FROM sales) AS t
GROUP BY bucket ORDER BY bucket;

-- Joins. Each table's own filters run in its index; inner joins start from
-- the table with the fewest matching rows, then fetch only the matching rows
-- of the other (an index union on the join key)
SELECT c.name, COUNT(*) AS orders, SUM(s.quantity) AS units
FROM sales s JOIN customers c ON s.customer_id = c.customer_id
WHERE c.country = 'JPN' AND s.status = 'shipped'
GROUP BY c.name ORDER BY units DESC LIMIT 3;

-- LEFT JOIN anti-join: customers who never bought 20 gizmos in one order
SELECT c.name FROM customers c
LEFT JOIN sales s ON s.customer_id = c.customer_id AND s.product = 'gizmo' AND s.quantity = 20
WHERE s.order_id IS NULL ORDER BY c.name LIMIT 5;

-- Comma joins with the condition in WHERE are hash joins, not cross products
SELECT c.country, COUNT(DISTINCT c.customer_id) AS customers, SUM(s.quantity * s.unit_price) AS revenue
FROM customers c, sales s WHERE s.customer_id = c.customer_id
GROUP BY c.country ORDER BY revenue DESC LIMIT 3;

-- Views. A single-table view stays lazy: filters on the view are pushed into
-- the base table's index together with the view's own WHERE
CREATE VIEW shipped_sales AS
SELECT order_id, customer_id, country, product, quantity FROM sales WHERE status = 'shipped';
SELECT COUNT(*) FROM shipped_sales WHERE country = 'JPN' AND quantity >= 15;
DROP VIEW shipped_sales;

-- Temporary tables and views belong to this connection: other connections
-- don't see them, and they are dropped when it closes
CREATE TEMP TABLE big_jpn AS
SELECT order_id, customer_id, quantity FROM sales WHERE country = 'JPN' AND quantity >= 18;
SELECT c.name, COUNT(*) AS orders FROM big_jpn b JOIN customers c ON c.customer_id = b.customer_id
GROUP BY c.name ORDER BY orders DESC, c.name LIMIT 3;

-- Set operations: each branch runs in its own index; the driver combines them
SELECT country FROM customers WHERE customer_id <= 10
UNION
SELECT country FROM sales WHERE quantity = 20 AND product = 'gizmo'
ORDER BY 1;
SELECT customer_id FROM customers
EXCEPT
SELECT customer_id FROM sales WHERE product = 'gizmo' AND quantity = 20
ORDER BY 1 LIMIT 5;

-- Window functions run in the driver, after WHERE / GROUP BY / HAVING.
-- Each customer's latest order (the dbt de-duplication idiom), and the same
-- with QUALIFY: the earliest customer per country
SELECT customer_id, order_id, ordered_at FROM (
  SELECT customer_id, order_id, ordered_at,
         ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY ordered_at DESC) AS rn
  FROM sales
) AS latest WHERE rn = 1 ORDER BY customer_id LIMIT 5;
SELECT name, country, signup_date FROM customers
QUALIFY ROW_NUMBER() OVER (PARTITION BY country ORDER BY signup_date) = 1 ORDER BY country;

-- Ranks and running totals over grouped rows; LAG and a moving average
SELECT country, SUM(quantity) AS units, RANK() OVER (ORDER BY SUM(quantity) DESC) AS rnk,
       SUM(SUM(quantity)) OVER (ORDER BY SUM(quantity) DESC) AS running_units
FROM sales GROUP BY country ORDER BY rnk;
SELECT order_id, quantity, LAG(quantity) OVER w AS prev,
       AVG(quantity) OVER (w ROWS BETWEEN 2 PRECEDING AND CURRENT ROW) AS moving_avg
FROM sales WHERE customer_id = 7 WINDOW w AS (ORDER BY order_id) ORDER BY order_id LIMIT 5;

-- Date/time functions (UTC): monthly totals for the first quarter
SELECT TO_CHAR(DATE_TRUNC('month', ordered_at), 'YYYY-MM') AS month, COUNT(*) AS orders, SUM(quantity) AS units
FROM sales WHERE EXTRACT(QUARTER FROM ordered_at) = 1
GROUP BY month ORDER BY month;

-- Timestamp arithmetic: the bound is computed once, then runs as an index range
SELECT COUNT(*) AS last_week FROM sales
WHERE ordered_at >= TIMESTAMP '2025-12-31 00:00:00' - INTERVAL '7 days';
SELECT MAX(ordered_at) - MIN(ordered_at) AS span, AGE(MAX(ordered_at), MIN(ordered_at)) AS age FROM sales;

-- Math, string and conditional functions. The index still answers the WHERE
-- clause; functions over columns run in the driver on the matching rows
SELECT INITCAP(product) AS product, ROUND(AVG(unit_price), 2) AS avg_price,
       ROUND(SUM(quantity * unit_price * (1 - COALESCE(discount, 0))), 2) AS net
FROM sales WHERE status = 'shipped' GROUP BY product ORDER BY net DESC;
SELECT LPAD(CAST(customer_id AS VARCHAR), 4, '0') AS id, SPLIT_PART(email, '@', 2) AS domain,
       SUBSTRING(name FROM 10) AS num, IIF(country IN ('USA', 'CAN', 'MEX'), 'americas', 'other') AS region
FROM customers WHERE customer_id <= 3 ORDER BY customer_id;

-- information_schema: which columns of sales are indexed?
SELECT column_name, data_type, is_nullable, is_indexed
FROM information_schema.columns WHERE table_name = 'sales' ORDER BY ordinal_position;

-- LIKE; a prefix pattern on an indexed column is an index prefix query
SELECT COUNT(*) FROM sales WHERE product LIKE 'gi%';

-- Regular expressions (RE2 syntax) and SIMILAR TO run in the driver, on the
-- rows the rest of the WHERE clause finds in the index
SELECT name, REGEXP_SUBSTR(email, '([0-9]+)@', 1, 1, '', 1) AS num, SUBSTRING(email FROM '@(.*)$') AS domain
FROM customers WHERE email ~ '^customer[0-9]@' ORDER BY customer_id LIMIT 3;
SELECT product, COUNT(*) AS orders FROM sales
WHERE country = 'JPN' AND product SIMILAR TO 'g(adget|izmo)' GROUP BY product ORDER BY product;

-- IN lists, dates
SELECT name, country, signup_date
FROM customers
WHERE country IN ('USA', 'CAN')
ORDER BY name
LIMIT 5;

-- Writes
INSERT INTO customers (customer_id, name, country, signup_date)
VALUES (51, 'Ada', 'GBR', DATE '2026-09-30');
UPDATE sales SET status = 'returned' WHERE order_id = 1;
DELETE FROM customers WHERE customer_id = 51;

-- DDL
CREATE TABLE events (id BIGINT NOT NULL, kind VARCHAR, payload VARCHAR NOINDEX, at TIMESTAMP(3));
COMMENT ON COLUMN events.payload IS 'Raw JSON, as received';
DROP TABLE events;
```

### 4. Inspect the data in Redis

Each row is a plain HASH, and each table has one RediSearch index. A renamed
table keeps its key names unless `adbc.redis.rename_rekey` is set (see
`ALTER TABLE` below), and a table created again after `DROP TABLE`, or
emptied by `TRUNCATE`, gets new ones (`public:sales~2:`, see "Key prefixes"
below); `information_schema.tables` shows each table's `key_prefix` and
`index_name`:

```bash
docker exec redis-adbc-test redis-cli HGETALL public:sales:42
```

```bash
docker exec redis-adbc-test redis-cli FT.INFO idx:public:sales
```

To see the `FT.AGGREGATE` / `FT.CURSOR` commands the driver sends for each query,
run `MONITOR` in a second terminal while you run SQL:

```bash
docker exec -it redis-adbc-test redis-cli MONITOR
```

Stop Redis with `docker compose down`.

## Existing HASH collections: `redis-adbc`

The driver only sees tables that have its metadata. HASHes that an
application wrote (`user:1`, `user:2`, …, maybe with the application's own
search index) can still be queried through the driver, made a table one of
two ways.
`redis-adbc` checks whether each one works, guesses the table's structure,
and says what is missing:

- **Copy.** `redis-adbc copy` reads the HASHes and bulk-ingests them into a
  new driver table, through the driver. The originals are left as they
  are. The table is a snapshot: copy again with `-mode replace` to refresh
  it. It needs the Query Engine, a database that isn't Redis Flex, and
  memory for a second copy of the data.
- **Adopt.** `redis-adbc adopt` makes the HASHes themselves a driver table:
  SQL reads what the application writes, including HASHes it writes later,
  without a copy. It writes nothing into the HASHes, and reads them through
  the application's own search index when that can serve (otherwise it
  creates one in the driver's format). Its columns are only as typed as
  the stored text allows (see [Adopt](#adopt-hashes-in-place) below for
  what an adopted table can't do).

`redis-adbc sweep` is for the driver's own tables rather than an
application's HASHes: it deletes the rows that writers left under the key
prefixes of dropped and truncated tables (see [Sweep](#sweep-leftover-rows)
below).

### Build and connect

Build it from `go`:

```bash
make adbc-cli
```

Or download `redis-adbc-<version>-<os>-<arch>.tar.gz` from a
[release](https://github.com/alberttwong/redis-adbc-driver/releases) after
v0.0.10. The Linux binaries are statically linked, and each release runs
them on the same distributions as the driver. On macOS, a binary from a
tarball downloaded with a browser is quarantined, and macOS refuses to run
it; download with `gh release download` or `curl` instead, or clear the flag
with `xattr -d com.apple.quarantine redis-adbc`.

`redis-adbc` links the driver as a Go library, so it needs neither cgo nor
the shared library. It connects with `-uri`, else `$REDIS_URI`, else
`redis://localhost:6379/0`. Set a password in `$REDIS_URI` rather than in
`-uri`, where other users of the machine can see it in the process list.
`-option key=value` (repeatable) sets a database option, such as
`-option adbc.redis.read_timeout=30m`. Flags go before anything else.

### Check collections

Without `-prefix`, `check` lists the collections it finds: the
prefixes (up to the last `:`) of HASH keys in a sample of 10,000 keys, the
prefixes of search indexes on HASHes, and the driver's own tables. With a
prefix, it reads a sample of the HASHes, guesses the columns (their type in
a copy, and in place), and prints a checklist for each way of making them
a table. Each item is `ok`, `warn`, `missing` (the suggested command
provides it) or `blocker` (something to change first), with a fix:

```bash
build/redis-adbc check -prefix user: -table users
```

```text
Collection user: on Redis 8.6.2 (standalone)
  501 HASH keys; the columns are guessed from 501 of them
  search index idx:users (prefix user:): 499 documents, 2 indexing failures

Guessed columns (as table public.users):
  COLUMN      COPY TYPE                    IN-PLACE TYPE     PRESENT  NOTES
  _key        VARCHAR                      -                 100%     each HASH's key
  name        VARCHAR                      VARCHAR           100%
  age         VARCHAR                      VARCHAR           100%     2 of 501 values aren't BIGINT (such as "n/a" in user:500)
  active      BOOLEAN                      VARCHAR           99.8%    true/false text
  created_at  TIMESTAMP(6) WITH TIME ZONE  VARCHAR           99.8%    ISO 8601 text
  balance     NUMERIC(38,2)                NUMERIC(38,2)     99.8%
  zip         VARCHAR                      VARCHAR           99.8%    numbers with leading zeros, kept as text
  avatar      VARBINARY                    VARBINARY         10.0%    not UTF-8; 50 values have a NUL byte

SQL, copied into a driver table (redis-adbc copy): READY
  [ok]      the Query Engine (RediSearch) is available
  [ok]      501 HASH keys under user:
  [ok]      the columns are guessed from all 501 HASHes
  [ok]      every field has a column type (8 columns)
  [warn]    age is VARCHAR: 2 of 501 values aren't BIGINT (such as "n/a" in user:500)
            fix: -type age=BIGINT -on-error null reads them as NULL
  [ok]      public.users doesn't exist yet: copy creates it
  [ok]      its rows go under public:users:, which has no keys, and its index is idx:public:users
  [ok]      a copy needs about 0.10 MB (MEMORY USAGE of the sampled HASHes, scaled to all of them), plus its index; 14 MB are in use, and there is no maxmemory limit
  run: redis-adbc copy -prefix user: -table users

SQL, adopted in place (redis-adbc adopt): READY
  [ok]      the Query Engine (RediSearch) is available
  [ok]      no driver table's rows can be under user:
  [ok]      public.users doesn't exist yet
  [ok]      every HASH under user: is a row (501)
  [warn]    active is VARCHAR in place (BOOLEAN when copied): the driver stores booleans as 0 and 1
  [warn]    created_at is VARCHAR in place (TIMESTAMP(6) WITH TIME ZONE when copied): the driver stores dates and times as numbers
  [warn]    the application's index idx:users can't serve: it has had 2 indexing failures, so it may leave HASHes out
  [missing] a search index in the driver's format on user:
            fix: adopt creates idx:public:users:adopted, over the HASHes that are rows, including those the application writes later; it takes memory of its own (the application's idx:users stays as it is, and isn't used)
  [missing] the table's metadata (adbc:{meta}:table:public:users)
            fix: adopt writes it
  [warn]    a write by the application of a value a numeric column can't hold (text, an empty value, NaN) takes that HASH out of the index, and so out of the table
  [warn]    SQL UPDATE and DELETE change the application's HASHes (an UPDATE that would leave a HASH without fields is refused); INSERT, TRUNCATE, ADD COLUMN and a re-keying RENAME are refused, and DROP TABLE drops only the metadata and an index adopt created
  run: redis-adbc adopt -prefix user: -table users
```

`-json` writes the same report as JSON. `check` writes nothing to Redis,
and exits 0 whenever it could check (whether or not a checklist is
ready). What the checklists look for:

- **Copy:** the Query Engine (`FT._LIST`); not Redis Flex (the probe the
  driver runs on connect); HASH keys under the prefix, and how many the
  columns were guessed from; columns that a few odd values made `VARCHAR`;
  `-type` overrides that some sampled values don't convert to; fields that
  can't be columns (`__rowid`, other names starting with `__`, an empty
  name); the target isn't a view (an existing table is a warning: copy
  with `-mode replace` or `append`); no keys already under the key prefix
  the new table would take (they would merge with its rows), which also
  rules out copying a collection into its own prefix; column names that
  can't be indexed (only letters, digits and `_` can, and at most 200
  columns); memory: the sampled HASHes' `MEMORY USAGE` scaled to all of
  them, against `used_memory`, `maxmemory` and `maxmemory_policy` (summed
  over a cluster's primaries). It is a blocker when the copy doesn't fit,
  or leaves less than 10% free under an `allkeys-*` policy (which could
  evict the originals); otherwise less than 10% free is a warning.
- **In place:** the Query Engine, and not Redis Flex; that no driver
  table's rows can be under the prefix (and that the prefix isn't the
  driver's own `adbc:` or `pg_temp_`); the table, view and index names;
  which HASHes would be rows: those with no `:` after the prefix, whatever
  their keys (`user:42`, `user:alice`), while keys such as `user:42:prefs`
  are another collection's, and stay out of the table; columns that are
  weaker in place than in a copy; `-type` overrides that some values
  aren't stored as; the application's index adopt would read the rows
  through and what its attributes answer, or why none can serve; what
  adopt creates; and the hazards below. For a table
  that was adopted already, it checks the index's indexing failures, and
  whether redis-adbc v0.0.11 adopted it (see below).

### Column inference

Every field becomes a column (`-rename field=column` renames one), plus, in
a copy, `_key` with each HASH's key
(`-key-column` names it; empty for none). A column's type is the narrowest that fits the
text of every sampled value, read as a SQL cast reads it:

| Values | Type |
|-|-|
| `true`, `false`, `t`, `f`, `yes`, `no` (any case) | `BOOLEAN` |
| integers that fit in 64 bits | `BIGINT` (only `0` and `1` too, with a note) |
| fixed-point numbers such as `12.50` (at most 9 decimals), and longer integers | `NUMERIC(38, s)`, s the most decimals seen |
| other numbers: exponents, more decimals, `NaN`, `Infinity` | `DOUBLE PRECISION` |
| `YYYY-MM-DD` | `DATE` |
| ISO 8601 dates and times (`T` or a space, optional fraction and offset) | `TIMESTAMP(p)`, or `TIMESTAMP(p) WITH TIME ZONE` if any has an offset (the rest are read as UTC); p is 6, or the most fractional digits seen if that is more |
| not UTF-8 | `VARBINARY` |
| anything else, mixed kinds, and integers with leading zeros (`007`) | `VARCHAR` |

An empty value doesn't count, and reads as NULL in a column that isn't a
string. A field that some HASHes lack is NULL there. When at most 5% of a
column's values don't fit the type the rest share, the column is `VARCHAR`,
and `check` suggests `-type col=TYPE -on-error null`. `-type col=TYPE`
sets any column's type (SQL type names, as in `CREATE TABLE`).

### Copy HASHes into a table

`copy` takes `check`'s `-prefix`, `-schema`, `-table`, `-sample`,
`-key-column`, `-type` and `-rename`, and bulk ingest's `-mode` (`create`,
the default, `append`, `replace` or `create_append`) and `-index-columns`:

```bash
build/redis-adbc copy -prefix user: -table users -type age=BIGINT -on-error null
```

```text
501 rows copied into public.users
```

- **Columns** are guessed from the first `-sample` HASHes (1,000 by
  default, `-1` for all of them) before the copy starts. A field that only
  later HASHes have isn't a column: `copy` skips it and names it at the end
  (`skipped fields that the sample didn't have …`). Every column is
  nullable.
- **A value that doesn't convert** to its column's type stops the copy with
  the key, the field and the value. With `-mode create` the table it made
  is dropped again; with the other modes the rows copied before stay. With
  `-on-error null` the value is copied as NULL instead, and counted at the
  end.
- **Consistency:** keys come from `SCAN` (on every primary of a cluster)
  and each HASH from `HGETALL`, so this isn't a snapshot. A HASH that
  exists for the whole copy is copied once (`SCAN` can return a key twice;
  `copy` remembers the keys it has read, which takes memory for every key).
  One created or deleted meanwhile may or may not be copied.
- **Memory:** the HASHes are read and ingested in batches of up to 10,000
  rows, whatever the collection's size.

### Adopt HASHes in place

`adopt` reads every HASH under the prefix, runs the in-place checklist on
all of them, and prints what it would do. Nothing changes
without `-apply`:

```bash
build/redis-adbc adopt -prefix user: -table users
```

```bash
build/redis-adbc adopt -prefix user: -table users -apply
```

```text
Adopted: public.users now reads the HASHes under user:, and those the application writes later (index idx:public:users:adopted).
```

- **What it does,** in order, when it creates the index (see "The
  application's index" below for when it doesn't):
  1. Reserve the key prefix and the index name `idx:<schema>:<table>:adopted`
     (no driver table's index can have that name). The transaction checks
     again that no driver table's prefix overlaps the prefix.
  2. Create the index: the driver's format (each column as a table's), over
     the existing keys, with a `FILTER` on `@__key` so that it only holds
     the HASHes that are rows (no `:` after the prefix). The fields that
     would set a document's language, score or payload are renamed to ones
     no application uses (`__adbc_language`, …).
  3. Wait until the index has indexed every HASH. If it couldn't index one
     (a value written meanwhile that a NUMERIC column can't hold), adopt
     drops the index (without `DD`) and gives the names back.
  4. Write the table's metadata, marked `adopted`.

  Adopt writes nothing into the HASHes. A HASH the application writes
  later is a row as soon as the index has it.

  An adopt that stopped half-way (the process was killed, say) left a
  record in `adbc:{meta}:adopting`. Running it again with the same `-table`
  finishes it. Adopting HASHes that are an adopted table already does
  nothing.
- **The application's index.** If a search index of the application's
  can serve, adopt reads the rows through it, and creates no index of its
  own (`Adopted: public.users now reads … (through the application's index
  idx:users)`). The driver never changes or drops that index. It can serve
  if it is on HASHes, its only prefix is the table's, it has no `FILTER`, no
  indexing failures and has finished indexing, and it holds a document for
  every HASH under the prefix (one made with `SKIPINITIALSCAN` may not).
  Its documents that aren't rows (`user:42:prefs`) are left out by a
  `FILTER` step in each query. Of several, adopt picks the one with an
  attribute for the most columns; `-index idx:users` names one, and
  `-own-index` creates the driver's index instead. Each statement reads
  the index's attributes (`FT.INFO`), and what runs in the index depends on
  them:

  | Attribute on the column's field | What runs in the index |
  |-|-|
  | `NUMERIC SORTABLE`, on a numeric column | filters, sorts, and aggregates (as on a table's own index) |
  | `NUMERIC`, on a numeric column | filters; the driver sorts and aggregates |
  | `TAG`, on a string column | `=`, `IN` and `LIKE 'x%'` find the rows that may match, and the driver checks them: the TAG's separator splits values into tags, and without `CASESENSITIVE` it ignores case. `= ''` needs `INDEXEMPTY`. The driver sorts and groups |
  | `TEXT`, `GEO`, `VECTOR`, `NOINDEX`, one named with `AS`, or none | nothing: filters on the column read every row |

  `check` lists which columns get which. `-index-columns` picks the columns
  of an index adopt creates, so it needs `-own-index` when the
  application's index can serve. If the application drops its index, or
  changes its prefixes or `FILTER`, statements on the table fail
  (`"public"."users" reads the application's search index idx:users, which
  is gone: …`) until it is made again as it was; `DROP TABLE` still works,
  and leaves the index alone.
- **Rows** are identified by their keys, which the system column `__key`
  holds: like `__rowid` in other tables, `SELECT *` leaves it out, but
  `SELECT __key` reads it, and `WHERE __key = 'user:42'` reads that HASH
  directly. Without `ORDER BY`, rows come in the index's order (there is
  no insertion order).
- **Columns.** Every field is a column, as in a copy, but in place a column
  is only `BIGINT`, `NUMERIC(38, s)` or `DOUBLE PRECISION` if every HASH
  that has the field holds such a number. The driver reads the stored
  text as it is: it stores booleans as 0 and 1, and dates and times as
  integers, so `true`/`false` text and ISO dates are `VARCHAR`. A field
  that is empty in some HASH is `VARCHAR`, since RediSearch doesn't index a
  HASH whose `NUMERIC` field isn't a number. `-type col=TYPE` sets a type
  that every value is stored as (`-type flag=BOOLEAN` for 0/1 flags), and
  `-index-columns` picks the indexed columns. Every column is nullable.
- **Strings.** The application's later writes don't tell the driver which
  strings the index doesn't hold exactly (see "Strings in the index"
  below), so an adopted table's indexed string columns count as
  `truncated`. The driver then re-checks string filters, and sorts, groups
  and reads string columns itself: always right, but slower.
  `-trust-strings` records the levels measured now instead, for HASHes the
  application no longer writes, or never writes with a string the index
  doesn't hold exactly.
- **What an adopted table refuses.** Its keys are the application's, so
  the driver doesn't make new ones, move them, or delete them wholesale:
  - `INSERT` (and `MERGE … INSERT` and bulk ingest), `TRUNCATE`,
    `ALTER TABLE … ADD COLUMN`, and `RENAME TO` with
    `adbc.redis.rename_rekey` fail with `… isn't supported on
    "public"."users", whose rows are HASHes adopted in place (redis-adbc
    adopt) under "user:"`.
  - `DROP TABLE` drops the adopted index (without `DD`), or none if the
    table reads the application's, and the metadata. The HASHes stay as
    they are.
  - `DROP COLUMN` forgets the column. The field stays in the HASHes.
  - No new table may have its rows under an adopted prefix: a schema named
    `user` would put a table's rows under `user:`. `CREATE TABLE` there
    fails.
  - `SELECT`, `UPDATE`, `DELETE`, views, joins and `ALTER TABLE … RENAME`
    work as on any table. `UPDATE` and `DELETE` change the application's
    HASHes.
  - An `UPDATE` that would set every field of a HASH to NULL fails with
    `UPDATE would leave the HASH user:42 of "public"."users" without
    fields, and Redis deletes a HASH without fields (and so the row) …`,
    before it changes anything.
- **Hazards,** which `check` and `adopt` list too:
  - An application write of a value that a numeric column can't hold (text,
    an empty value, `NaN`) takes that HASH out of the index, and so out of
    the table. `FT.INFO` counts it as an indexing failure, and `check`
    reports it. With the application's index, a value that one of its
    `NUMERIC` attributes can't hold does the same.

#### Tables adopted by redis-adbc v0.0.11

v0.0.11 identified an adopted table's rows by a `__rowid` field, which it
wrote into each HASH whose key
ended in an integer, and its `adopt -refresh` wrote it into HASHes added
later. The driver now refuses such a table:

```text
"public"."users" was adopted by redis-adbc v0.0.11, whose adopted tables this driver no longer reads: DROP TABLE "public"."users" (its HASHes stay) and run redis-adbc adopt again
```

`DROP TABLE` still works, and `check` and `adopt` say the same. The
`__rowid` fields stay in the HASHes; they aren't columns, and do no harm.

### ACL permissions for `redis-adbc`

`check` needs read access to the collection's keys (`%R~user:*`) and to the
driver's metadata (`%R~adbc:*`), and these
commands:

```text
+ping +info +scan +hkeys +hmget +ft._list +ft.info +ft.aggregate +memory|usage
+get +exists +smembers +sismember +hget +hexists
# A cluster (OSS Cluster API) also
+cluster|slots +command
```

`check` leaves out the search indexes on keys the user may not read (Redis
refuses `FT.INFO` on them), and says how many. Without `MEMORY USAGE`, it
warns that it can't estimate the memory a copy needs, and goes on.

`copy` and `adopt` need the driver's own rules (see
[ACL users](#acl-users)). `copy` and `adopt` also need
`+scan +hkeys` and read access to the collection's keys (`%R~user:*`):

```text
+scan +hkeys
```

`HMGET` is already in the driver's rules; the tools use `HKEYS` followed by
`HMGET` so inferred column order is the same under RESP2 and RESP3.

A user that only queries an adopted table needs the driver's read-only
rules, with read access to the collection's keys (`%R~user:*`), and
`+ft.info` if the table reads the application's index (each statement
reads its attributes). One that
updates or deletes its rows needs the driver's rules, write access to the
keys (`~user:*`), and, for the script that `UPDATE` runs on each HASH:

```text
+hlen +eval +evalsha +script|load
```

### Sweep leftover rows

`DROP TABLE`, `TRUNCATE` and a re-keying rename release the table's key
prefix, and no table takes it again (see "Key prefixes are
never reused" below). Rows can still be left under it, where no table or
query reads them: those of a writer whose process exited before it found
out, and of a write of 1,000 rows or fewer that read the metadata within
100 ms of the release and reached Redis after it. `sweep` finds them, and
with `-apply` deletes them. Nothing is deleted without `-apply`:

```bash
build/redis-adbc sweep
```

```text
Released key prefixes on Redis 8.6.2 (standalone): 4
  PREFIX            HASHES
  public:events:    1
  public:orders~2:  12

Left alone:
  user:  an adopted table had it: the HASHes are the application's

Nothing was deleted. With -apply, sweep deletes the 13 HASHes under 2 prefixes.
```

```bash
build/redis-adbc sweep -apply
```

```text
Released key prefixes on Redis 8.6.2 (standalone): 4
  PREFIX            HASHES  DELETED
  public:events:    1       1
  public:orders~2:  12      12

Left alone:
  user:  an adopted table had it: the HASHes are the application's

Deleted 13 HASHes under 2 prefixes.
```

- **What it deletes:** the HASHes whose key is a released prefix and a row
  id (`public:orders~2:42`), found with `SCAN … TYPE hash` (on every
  primary of a cluster). The prefix is the part of the key up to its last
  `:`, so the rows of a live table whose name starts with the dropped
  one's (`public:orders2:`), or of the table's next prefix
  (`public:orders~3:`), are never touched, nor are keys under the prefix
  that aren't a row id or aren't HASHes.
- **What it leaves alone,** listed with the reason:
  - Prefixes that a table has. Up to v0.0.7, the driver gave a dropped
    table's prefix to the next table of its name. `sweep` checks again
    before each pipeline of 1,000 deletions.
  - The prefixes of a re-keying rename that is still moving rows: a `DROP
    TABLE` during one releases the table's prefix early. Run `sweep` again
    once the rename has finished. The next connection to open settles one
    whose connection went away.
  - The prefixes of adopted tables, also of dropped ones (listed in
    `adbc:{meta}:adopted:dropped`), and the prefixes that overlap them:
    those HASHes are the application's. v0.0.11 and v0.0.12 didn't record
    dropped adopted tables, so `sweep` also leaves alone every prefix that
    doesn't have the form of a driver table's, `<schema>:<table>:`
    (`user:`, `app:user:profile:`). An application's prefix that has that
    form (`app:users:`), adopted and then dropped with those versions,
    can't be told apart: check the dry run before `-apply`.
  - Prefixes whose keys the user may not delete (`NOPERM`).
  - Temporary tables' prefixes, which aren't counted as released: a
    connection that opens drops the temporary tables of connections that
    went away, rows and all.
- **Cost:** `SCAN` goes through every key of the database (it returns
  those under the released prefixes' longest common prefix, such as
  `public:`), which takes a while on a large one. So nothing sweeps when a
  connection opens.
- **Writers still running** may leave keys after `sweep` has passed them.
  Running it again later deletes those. The counts in
  `adbc:{meta}:released` stay, so no new table takes the prefixes.
- `-json` writes the result as JSON. `sweep` exits 0 when it could sweep,
  whether or not it found HASHes, and 1 on an error, after printing what it
  found and deleted until then.
- **ACL users:** `sweep` needs the driver's own rules, with `+scan`, and
  write access to the keys of the schemas it sweeps (`~public:*`).
- **Earlier versions** (v0.0.12 and before) have no `sweep`. It works on
  databases they wrote, except for the prefixes that v0.0.5 and earlier
  released: they didn't record them.

## Architecture: hybrid index-row layout

```
          ┌───────────────────────────────────────────────┐
          │ Secondary index (RediSearch)  idx:<s>:<t>     │
          │ only filter/sort/aggregate columns, SORTABLE  │
          └───────────────┬───────────────────────────────┘
  finds matching rows     │   computes GROUPBY/REDUCE without
  (filter, sort, limit),  │   opening the HASHes
  LOAD reads their fields ▼
┌──────────────────────────────────────────────────────────────────┐
│ Primary data store: one flat HASH per row (every column)         │
│ public:sales:1 ─► { __rowid: 1, country: "USA", amount: 45, ... }│
└──────────────────────────────▲───────────────────────────────────┘
                               │ direct HMGET (full-row lookup)
```

### Rows and indexes

- **Rows** are flat HASHes `<schema>:<table>:<rowid>` holding every column
  (NULL = field absent, except for missing values, below; a hidden `__rowid`
  field keeps all-NULL rows alive). A table that isn't the first of its
  name, or that has been truncated, has the prefix `<schema>:<table>~N:`
  and the index `idx:<schema>:<table>~N` (see "Key prefixes" below).
- **Selective index**: `FT.CREATE idx:<schema>:<table> ON HASH PREFIX 1
  <schema>:<table>:` covering only filterable columns: numeric, boolean,
  decimal, date/time and timestamp columns as `NUMERIC SORTABLE`, strings as
  `TAG SEPARATOR "\x1f" CASESENSITIVE INDEXEMPTY SORTABLE UNF`. Binary,
  `INTERVAL` and `TIME WITH TIME ZONE` columns, and columns declared
  `NOINDEX`, are stored in the HASH only.
- **Strings in the index**: a TAG doesn't hold every string as it is.
  RediSearch cuts a value at its first NUL byte, splits it at the separator,
  trims ASCII whitespace (space, `\t`, `\n`, `\v`, `\f`, `\r`) from both
  ends of each piece and cuts each piece to 4,096 bytes: so `'ab '`, `' ab'`
  and `'ab'` all have the tag `ab`, and `' '` has the tag `''` (Unicode
  spaces are kept). The sort vector holds the whole value, except that it
  also stops at a NUL byte. So each indexed string column's metadata
  records whether the index holds all its values exactly, or some only as
  other tags (`tag_values: normalized`), or some cut at a NUL byte
  (`truncated`). Every write (`INSERT`, `UPDATE`, `MERGE`, bulk ingest,
  `ADD COLUMN … DEFAULT`) raises it before writing the rows, and it never
  goes down (not even on `DELETE` or `TRUNCATE`). Then:
  - On a column held exactly, `col = 'v'` is the exact TAG query `@c:{v}`,
    so `COUNT`, aggregates and `LIMIT` still run in the index.
  - Otherwise, and for a constant that isn't held exactly, the query looks
    up the constant's tag (`@c:{ab}` for `'ab '`) and the driver re-checks
    the rows, so the index no longer counts, aggregates or limits on its
    own for that predicate. `IN` lists, index lookup joins and `LIKE`
    prefixes look tags up the same way (and are always re-checked).
  - A truncated column is also sorted, grouped and read (`LOAD *`) by the
    driver. A constant with an ASCII control character other than
    whitespace can't be written in a TAG query, so it is checked by the
    driver.
  - Tables created by earlier versions (no `tags_checked` in their
    columns' metadata) count as truncated until a background check,
    started by the first statement that reads one on a connection, has read
    every row (about 0.4 s for 100,000 rows of 10 columns) and recorded
    what they hold. It records nothing if the table's metadata changed
    meanwhile (a writer raising a level), and starts over; a connection
    whose check failed tries again a minute later.
  - A statement that read a table's metadata before another connection
    raised a level may see that connection's new rows as the index holds
    them (count a new `'ab '` as `'ab'`, or sort or read a new value with a
    NUL byte up to that byte), in the same window in which it may or may
    not see them at all. Earlier versions of the driver don't raise levels
    when they write.
- **NaNs in the index**: `REAL` and `DOUBLE PRECISION` columns hold `NaN`,
  `Infinity` and `-Infinity`, as in Postgres. RediSearch doesn't index a
  HASH whose `NUMERIC` field is `NaN`, so up to v0.0.8 a row holding one was
  written and never seen again: no index query found it (only `WHERE
  __rowid = N` did), so `SELECT`, `COUNT(*)`, `UPDATE` and `DELETE` skipped
  it, and `DROP TABLE` and `TRUNCATE` left its HASH behind. A NaN is now
  stored as `infinity`, a spelling of +∞ that RediSearch indexes as `+inf`
  but that is neither what the driver writes for `Infinity` (`+Inf`) nor
  what RediSearch replies with (`inf`), so it reads back as NaN (as does the
  `NaN` of earlier versions). Postgres puts NaN above every number,
  `Infinity` included, and NaN equals NaN; in the index a NaN is `+inf`,
  which is the right place against every value but `Infinity`. So:
  - A comparison with a finite number or `-Infinity` runs in the index
    exactly, as before: `x > 5` finds the NaNs, `x < 5` doesn't. One with
    `NaN` or `Infinity` gets the inclusive bound `+inf` (`x = 'NaN'` is
    `@x:[+inf +inf]`) and is re-checked by the driver, as are `IN` lists,
    index lookup joins and semi-joins with those values. Index queries
    write infinite bounds as `+inf` and `-inf`: `x = 'NaN'` used to fail
    with `Syntax error … near NaN`.
  - Each indexed float column's metadata records whether it may hold a NaN
    (`nans`). Writes set it as they raise string levels, in the same
    transaction before the rows are written, and it is never cleared (not
    even by `DELETE` or `TRUNCATE`). On such a column the driver sorts
    (`ORDER BY`), groups (`GROUP BY`, `DISTINCT`) and, with
    `adbc.redis.aggregate_pushdown` `all`, computes `MIN`, `MAX`, `SUM` and
    `AVG` itself, since the index takes a NaN for `Infinity`; `COUNT(x)`
    still runs in the index. As with string levels, a statement that read
    the metadata before another connection's first NaN may sort or
    aggregate that connection's new NaNs as `Infinity`.
  - A hash join now matches a NaN with a NaN (it matched it with nothing),
    and `IN` sets and semi-joins look NaNs up by key, as `DISTINCT` and set
    operations did, instead of falling back to comparing values one by one.
  - **Rows written before:** a row that v0.0.8 or earlier wrote with a NaN
    in an indexed float column is still not in the index, and its HASH stays
    behind when the table is dropped or truncated. A table has such rows
    when its `COUNT(*)` is less than the number of keys under its
    `key_prefix` (`redis-cli --scan --pattern '<prefix>*' | wc -l`, on every
    primary of a cluster); `FT.INFO <index>` reports them as
    `hash_indexing_failures` (`Invalid numeric value: 'NaN'`). Writing the
    row again by its id indexes it: `UPDATE t SET x = 'NaN' WHERE __rowid =
    N`.
- **Ties in the sort vector**: the index sorts `NUMERIC` attributes as
  doubles, which hold every integer below 2^53 and every decimal of up to
  15 significant digits apart from its neighbours, but not larger `BIGINT`
  values, `TIME` and `TIMESTAMP` values as large in their unit (a
  `TIMESTAMP(9)` after April 1970, a `TIMESTAMP` after 2255 or before 1685)
  or `NUMERIC` values with more digits (from 10⁶ on for `NUMERIC`, which is
  `NUMERIC(38,9)`): 2^53 and 2^53 + 1 are the same double. The index
  returns rows that tie in the order it indexed them (reversed for
  `DESC`), so up to v0.0.12 `ORDER BY id` could return 9007199254740993
  before 9007199254740992, and `LIMIT 1` pick the wrong one. Rounding never
  swaps two values, so only rows that tie can be out of order:
  - The driver reads the sort keys the index returns with each row
    (`__sort0`, …). Rows that follow each other with the same keys, up to
    the first one that may round, are a run. A run of two or more rows with
    a key from 2^53 on (10^(15-s) for `NUMERIC(p,s)`) is sorted by the
    rows' values, read with `HMGET` for columns the query doesn't select.
    The rows still stream, a run at a time.
  - With `LIMIT` / `OFFSET` in the index, the driver asks for one row more
    on each side. If a run with such a key goes on past them, it reads the
    rest of the run: by value when the first `ORDER BY` key is a `BIGINT`,
    `TIME` or `TIMESTAMP` below 2^63 (`@c:[v v]`, and an `FT.SEARCH …
    LIMIT 0 0` that counts the rows before it if the run started before the
    rows read), otherwise by asking again with more rows on that side. The
    rows of the `LIMIT` are read before the first is returned.
  - Without such ties nothing changes but the two extra rows: the same
    commands run, and `LIMIT` still runs in the index. Microsecond
    timestamps between 1685 and 2255 never tie, and nanosecond ones only
    within about 256 ns of each other.

### Metadata and key naming

- **Metadata**, all in one hash slot: `adbc:{meta}:table:<schema>:<table>`
  (column types, defaults, missing values, string levels, NaN flags,
  comments, `CHECK` constraints and the primary key as JSON), `adbc:{meta}:seq:*` (row ids),
  `adbc:{meta}:tables:<schema>`, `adbc:{meta}:schemas`, and for views
  `adbc:{meta}:view:<schema>:<view>` (the SELECT text, its columns and comments) and
  `adbc:{meta}:views:<schema>`. Tables and views share one namespace.
  Each table's metadata records its row key prefix and index name, and
  `adbc:{meta}:prefixes` / `adbc:{meta}:indexes` reserve them, so a renamed
  table's rows can never be shared with a new table of the old name.
  `adbc:{meta}:names:next` holds the last `~N` handed out for each table
  name (see "Key prefixes" below).
  `adbc:{meta}:cleanup` lists tables with a dropped-column cleanup in progress.
  `adbc:{meta}:rekey` records renames that are moving a table's rows
  (`adbc.redis.rename_rekey`), and `ALTER COLUMN … TYPE`s rewriting them
  (marked `rewrite`), keyed by the old prefix, and
  `adbc:{meta}:rekey:alive:<id>` (30-second TTL, renewed by the renaming
  connection) shows that the connection doing one is alive. A record taken
  over from a connection that went away is marked `recovering`.
  `adbc:{meta}:released` counts how often each key prefix was released (by
  `DROP TABLE`, `TRUNCATE`, a re-keying rename or `ALTER COLUMN … TYPE`),
  one field per prefix that was ever released; a statement checks it and
  `adbc:{meta}:prefixes` to tell when its table's rows have moved away.
  Metadata written by
  v0.0.1 (`adbc:meta:*`, `adbc:schemas`, …) is migrated automatically on
  the first connection.
- **Key prefixes are never reused.** A table takes the first of
  `<schema>:<table>:`, `<schema>:<table>~2:`, … that no table has had
  before, with the index name of the same N (`idx:<schema>:<table>~2`).
  So a table created again after `DROP TABLE` gets new keys, and so do a
  table emptied by `TRUNCATE`, a table whose rows `ALTER COLUMN … TYPE`
  rewrote, and a re-keying rename onto a name that a table had before. The
  reason is writes still running when a table is
  dropped: a statement that read the table's metadata before the `DROP`
  goes on writing rows under the old prefix until it finds out, and those
  rows outlive the `DROP`, which only deletes the rows its index knows
  about. Up to v0.0.7 the next table of that name took the same prefix,
  and its row ids started at 1 again, so its rows were written into those
  leftover HASHes: a column that should have been NULL read the dropped
  table's value. Now:
  - **`TRUNCATE`** moves the table to new names, as if it were dropped and
    created again with the same metadata (see `TRUNCATE` in "Supported
    SQL"), so the checks below apply to statements still writing to it.
    Up to v0.0.7 it kept the prefix: rows such a statement wrote after the
    `TRUNCATE` stayed in the table (some not indexed, until an `ADD COLUMN`
    indexed them), and after `RESTART IDENTITY` the row ids started at 1
    again, so later inserts were written into those rows' HASHes. Rows were
    lost, and new rows read the writer's values for their NULL columns.
  - An insert reads the table's metadata again in the `MULTI` that
    allocates its row ids (each Arrow batch, in bulk ingest), at no extra
    cost. If the table was dropped, truncated, renamed or created again
    since the statement started, it writes nothing.
  - A write of more than 1,000 rows (`CREATE TABLE … AS`, `INSERT …
    SELECT`, bulk ingest, a large `UPDATE` or `MERGE`) also checks that the
    table still has its keys: between pipelines of 1,000 rows once 100 ms
    have passed since the last check, and always after its last pipeline
    (one round trip each). Smaller writes check at the end only when 100 ms
    have passed since they read the metadata, as before, and so do deletes
    of any size, which make no key.
  - A statement whose table was dropped or truncated (or whose rows a
    re-keying rename or `ALTER COLUMN … TYPE` moved away) stops and fails:
    "table … was renamed with its rows moved to new keys, rewritten by
    ALTER COLUMN … TYPE, truncated or dropped, while this statement was
    writing to it; some of its changes may be lost or have gone to another
    table". If no table has the old prefix any more, it first
    deletes every key it wrote there (one pipelined `DEL` per 1,000 keys).
  - Keys can still be left under the old prefix of a dropped or truncated
    table, where no table or query reads them: when the writing process
    exits before it finds out, and when a write of 1,000 rows or fewer that
    read the metadata within 100 ms of the `DROP` or `TRUNCATE` reaches
    Redis after it (it doesn't check, and succeeds, as if it had run
    before). `redis-adbc sweep -apply` deletes them (see
    [Sweep](#sweep-leftover-rows)). Nothing deletes them by itself: a
    connection that opens doesn't look for them, since that means reading
    every key.
  - **Upgrading:** tables created by earlier versions keep their names
    (until a `TRUNCATE` gives them new ones) and work as before. Prefixes
    released by v0.0.6 and v0.0.7 are recorded in `adbc:{meta}:released`
    and are skipped too, so rows they left behind can't reach a new table;
    v0.0.5 and earlier didn't record releases. Upgrade every client that
    writes to the database: an earlier version still gives a dropped
    table's prefix to the next table of that name, and empties a table in
    place, so its `TRUNCATE … RESTART IDENTITY` can still lose rows that
    another connection is writing.
- **Indexes the driver didn't create are left alone.** Before it creates a
  table's index (`CREATE TABLE`, bulk ingest, `TRUNCATE`, a re-keying
  rename), the driver drops an index that already has the new name with
  `FT.DROPINDEX … DD`: one left over from a `CREATE` or `DROP` that was
  interrupted. It now first checks with `FT.INFO` that the index is one:
  a HASH index on just the new prefix, without a filter, with no
  documents. Any other index of that name (an application's own
  `idx:public:users` on `user:`, say) makes the driver take the next names
  (`public:users~2:`, `idx:public:users~2`). Up to v0.0.10 it dropped that
  index, and `DD` deleted every HASH the index covered.
- **Missing values**: `ALTER TABLE … ADD COLUMN c … DEFAULT v` doesn't
  rewrite the existing rows. As with Postgres's "missing value", the
  column's metadata records `v` and the row id high-water mark (the
  `adbc:{meta}:seq:*` value, read in the transaction that adds the column),
  and a row with `__rowid` up to the mark and no `c` field reads `v`. Rows
  inserted later have higher ids, so for them an absent field is NULL as
  usual. To keep that true:
  - A statement that read the metadata before the `ALTER` allocates its row
    ids in one `MULTI` with a read of the metadata, sees the new column, and
    writes `v` for it.
  - Setting an old row's `c` to NULL (`UPDATE`, `MERGE`) also writes a
    hidden marker field, `__null_<field>`, which setting a value removes.
  - The index has no entry for an old row's absent field, so a filter on `c`
    that `v` satisfies is widened to `(@c:[…] | @__rowid:[-inf mark])` and
    re-checked by the driver (one that `v` doesn't satisfy stays an exact
    index query), and `ORDER BY` and aggregates on `c` run in the driver.
  - `TRUNCATE` drops the missing values with the rows.
- **Temporary tables and views** use the same layout in a private schema
  per connection, `pg_temp_<id>` (rows `pg_temp_<id>:<table>:<rowid>`, index
  `idx:pg_temp_<id>:<table>`), which is never added to
  `adbc:{meta}:schemas`. The connection id comes from the counter
  `adbc:{meta}:temp:next` when the connection creates its first temporary
  object. `adbc:{meta}:temp:owners` lists the connections that have a
  temporary schema, and `adbc:{meta}:temp:alive:<id>` shows that the owner
  is still open: it has a 2-minute TTL that the connection refreshes every
  30 seconds. Closing the connection drops its temporary objects and these
  keys. A new connection drops the temporary objects of any owner whose
  alive key has expired, re-checking the key before each object.

### Query execution

The driver executes SQL as follows:

| Query shape | Execution |
|-|-|
| `WHERE __rowid = N` | Direct `HMGET` of the row HASH, index bypassed |
| Queries that can't return rows: `WHERE false`, `LIMIT 0`, … | Their rows aren't read. A query with `LIMIT 0` (or `FETCH FIRST 0 ROWS ONLY`, or a `HAVING` or `QUALIFY` that is never true) returns without a command. One whose `WHERE` is never true doesn't read its FROM items, and the rest of it runs over no rows: `COUNT(*)` without `GROUP BY` still returns one row, 0. A condition is never true when it is FALSE or NULL whatever the rows hold: its parts that read no column, parameter or subquery are evaluated once, and `AND` is never true if one side is, `OR` if both are (`false`, `1 = 0`, `NULL`, `x > 0 AND false`, `NOT true`, …). This applies at every level (derived tables, CTEs, views, join items, `UNION` branches, subqueries) and to `INSERT … SELECT`, `UPDATE`, `DELETE`, `MERGE` and `CREATE TABLE … AS`. The derived tables, CTEs and views such a query reads are planned but not run, so its columns and their types are those of the full query, and unknown tables, columns and functions and wrong argument counts are still errors; `ExecuteSchema` and `CREATE VIEW` plan their query the same way. This is how dbt asks for a query's columns (`select * from (…) as __dbt_sbq where false limit 0`, for model contracts, snapshots and unit tests), and what `dbt run --empty` reads (`(select * from t where false limit 0)`) |
| Filter / sort / limit | `FT.AGGREGATE <idx> "<pushed-down query>" [SORTBY …] [LIMIT …] LOAD … WITHCURSOR COUNT 10000`: the rows come back in the cursor pages, up to 10,000 at a time (fewer for wide tables), and are [streamed](#streamed-results) as the result is read |
| `COUNT(*)`, `GROUP BY` + `COUNT/SUM/AVG/MIN/MAX`, `BOOL_OR/BOOL_AND/EVERY` of a boolean column | `FT.AGGREGATE … APPLY exists(@c) … GROUPBY … REDUCE …` over SORTABLE fields, HASHes never opened (`BOOL_OR` / `BOOL_AND` are `MAX` / `MIN` of the stored 0 and 1). On a cluster, a shard whose rows of a group all lack the value sends NaN as its partial `SUM`, which made the group's `SUM` and `AVG` NaN up to v0.0.7 (`AVG` of an integer column also in the default mode). So when a `SUM` comes back NaN for a group that has values, the driver runs the command again with `case(exists(@c), @c, 0)` in place of `@c` (about 2.5 times as long; a real NaN, from adding +Infinity and -Infinity, stays NaN), and a server without `case()` aggregates the query in the driver. An amd64 server writes that NaN `-nan`, which up to v0.0.9 the driver didn't read, so it aggregated such queries itself |
| `COUNT(DISTINCT col[, …])` | Eligible queries use exact server-side tuple deduplication, then count tuples in the driver; NULL tuples are excluded. See Aggregate pushdown below |
| Other aggregates (`STRING_AGG`, `STDDEV`, percentiles, `ANY_VALUE`, …), other `DISTINCT` aggregates and `FILTER (WHERE …)` | Reduced by the driver over the rows fetched from the HASHes (the index still filters them). An aggregate's `FILTER` is evaluated before its arguments, so `SUM(1 / x) FILTER (WHERE x <> 0)` never divides by zero. Ordered-set aggregates keep each group's values and sort them once |
| `GROUP BY ROLLUP` / `CUBE` / `GROUPING SETS` | Each grouping set runs as its own grouped query: an `FT.AGGREGATE … GROUPBY` when a plain `GROUP BY` of its columns would be one, otherwise in the driver, where the sets share one read of the rows. The driver combines the groups of all sets (`UNION ALL`, NULL for the columns a set doesn't group), then applies HAVING, window functions, QUALIFY, DISTINCT, ORDER BY and LIMIT to the combined rows |
| `col LIKE 'abc%'` on an indexed string column | TAG prefix query `@c:{abc*}` on the prefix's tag, re-checked by the driver (other patterns are checked by the driver alone). RediSearch expands a prefix into at most `search-max-prefix-expansions` tags (200 by default, per shard) and silently ignores the rest, so the driver first runs the prefix query under `FT.PROFILE … LIMIT 0 1` (one more round trip, about 0.3 ms) and, unless every shard's profile reports no warning, checks the prefix on every row the rest of the WHERE clause selects instead. The server's configuration is never changed |
| `col IN (…)`, `col IN (SELECT …)`, `col = a OR col = b` on an indexed column | Index union query (`(@c:[a a] \| @c:[b b])` or `@c:{a \| b}`), for up to 1,000 values. Above that (or on an unindexed column) the driver checks each row against a hash set of the values, built once per statement: always for `IN (SELECT …)`, and for a literal list of 16 or more constants when the column's type can't fail to compare with them (otherwise value by value, as `=` would) |
| Subqueries | Uncorrelated: run once per statement, results reused. Correlated `[NOT] EXISTS` / `[NOT] IN` over one table, CTE or derived table, whose only references to the outer query are `inner = outer` conditions in its WHERE: a hash semi-join (anti-join for `NOT`), which runs the subquery once without those conditions. A small inner side (≤ 1,000 rows, counted by the index) is read first, and then `EXISTS` / `IN` also filter the outer table in its index (nothing matches if the subquery has no rows; a union of ≤ 1,000 keys on an indexed outer column); a larger one is read only once running per outer row has cost about as much. Other correlated subqueries: run per outer row with the outer values as constants (so they still use the index), memoised |
| `(a, b) [NOT] IN (SELECT …)` (and `= ANY`, `<> ALL`) | The subquery runs once, and each row is checked against a hash set of its rows, built once per statement. Telling NULL from false (for `NOT IN`) costs one lookup per pattern of NULLs among the subquery's rows. If the subquery has no row without a NULL, `IN` reads no row. For each indexed column among the items with ≤ 1,000 distinct values in the subquery's rows, an index union fetches only the rows with those values. Correlated, with only `inner = outer` conditions: the hash semi-join below, with a set of rows per key. A literal list, `(a, b) IN ((1, 'x'), (2, 'y'))`, is an `OR` of `AND`s; each indexed column that every row of the list sets gets an index union (for up to 1,000 rows). A list of 16 or more rows of constants is checked against a hash set of its rows, as `IN (SELECT …)` is, when the items' types can't fail to compare with the values in their position (otherwise row by row, as the `=`s would) |
| `(a, b) > (x, y)`, `>=`, `<`, `<=`, and row `BETWEEN` | When `a` is an indexed numeric, date or time column, the index takes the bound the comparison implies on it, `a >= x` (`a <= x` for `<` and `<=`), and the driver checks the whole comparison on the rows the index finds. So keyset pagination, `WHERE (ts, id) > (?, ?) ORDER BY ts, id LIMIT n`, reads the rows from the last page's `ts` on, not every row before them too. Strings are only looked up by equality, so a string `a` is checked on every row |
| `x op ANY / ALL (SELECT …)` | `= ANY` is `IN` and `<> ALL` is `NOT IN`, with the same index unions, hash sets and semi-joins. Other operators: an uncorrelated subquery runs once, and `<`, `<=`, `>`, `>=` compare with its smallest or largest value (when all the values are of one type class); otherwise each row is compared with every value |
| `UNION` / `INTERSECT` / `EXCEPT` | Each branch runs as its own query (using its own index); the driver combines, de-duplicates and sorts the results |
| CTEs, derived tables | Run once, unless the query that reads them can't return rows (see above); the outer query filters, sorts and groups them in memory. One that reads an enclosing query's columns runs when the query that reads it does instead, once per distinct set of the values it reads (memoised like a correlated subquery, and shared by a CTE's references) |
| `WITH RECURSIVE` | Working-table iteration in memory: the non-recursive term runs once, then the recursive term runs against the rows the last iteration added (its joins with tables still use their indexes, such as an index lookup join on the new rows' keys) until it adds none. `UNION` drops rows produced before. At most 10,000 iterations and 1,000,000 rows. With `SEARCH` / `CYCLE`, the recursive term runs once per row of the working table, to know each row's parent |
| `LATERAL`, `GENERATE_SERIES` | Join items computed while the join runs. One that reads earlier FROM items runs for each row joined so far (a `LATERAL` subquery memoised on the values it reads, like a correlated subquery); otherwise it runs once and is joined like a derived table. A join with such an item keeps its written order. `GENERATE_SERIES` makes its rows in memory, at most 1,000,000 per call |
| `SELECT DISTINCT` | Over plain expressions it is the same as `GROUP BY` them, so it runs in the index (`FT.AGGREGATE … GROUPBY`) when they're indexed columns. Otherwise (stars, aggregates, window functions, `DISTINCT ON`) the driver removes duplicate rows after the rest of the query, NULLs counting as equal. `LIMIT` / `OFFSET` apply afterwards and never run in the index. As in Postgres, `ORDER BY` must use the select list, and `DISTINCT ON` keys must match the leading `ORDER BY` expressions |
| Window functions | Computed by the driver once the rows are known: after WHERE, GROUP BY and HAVING (which still run in the index when they can), before QUALIFY, ORDER BY and LIMIT. Rows are hashed into partitions and each partition is sorted once per distinct PARTITION BY / ORDER BY; frame aggregates add and remove rows as the frame slides (O(1) amortized per row; sums, averages and variances add and subtract exact sums, also of doubles). With `EXCLUDE CURRENT ROW / GROUP / TIES` the frame has a hole, so each row's frame is aggregated afresh (O(frame) per row), as PostgreSQL does; `FIRST_VALUE` / `LAST_VALUE` / `NTH_VALUE` stay O(1), also with `IGNORE NULLS`. A query with window functions never pushes its LIMIT into the index |
| `CREATE TEMP TABLE` / `VIEW` | Same as a permanent table or view, in the connection's `pg_temp_<id>` schema. Unqualified names are looked up there first (in memory, no extra round trip) |
| `TRUNCATE` | Like `DROP TABLE` and `CREATE TABLE` with the same metadata: reserves a new key prefix and index name and runs `FT.CREATE` for each table, switches the metadata of every table of the statement to the new names in one `WATCH`/`MULTI` (which also releases the old names), then runs `FT.DROPINDEX <old> DD`, which deletes every row the old index knows about. Statements still writing to the table find out and fail (see "Key prefixes are never reused") |
| `ALTER TABLE` | Metadata only (optimistic `WATCH`/`MULTI` on the table's metadata, one for all the actions of a statement), plus `FT.INFO` and one `FT.ALTER` for the added columns and a background `HDEL` pass for `DROP COLUMN`. `ADD COLUMN … DEFAULT` records a missing value instead of writing the rows. Adding a `CHECK` (`ADD COLUMN … CHECK`, `ADD CONSTRAINT … CHECK`) or a `NOT NULL` (`ALTER COLUMN … SET NOT NULL`, `ADD COLUMN … NOT NULL` without a default, and the columns of `ADD PRIMARY KEY`) first reads the columns they use from every row, with one `FT.AGGREGATE` cursor scan for all the statement's new constraints, to check them, and when it commits, the rows inserted since (by `__rowid`). With `adbc.redis.rename_rekey`, `RENAME TO` also creates the new name's index, copies every row the old index lists with pipelined `DUMP` / `RESTORE … REPLACE` (a cursor page at a time; one key per command, so it works on a cluster), switches the metadata in one transaction, then runs `FT.DROPINDEX <old> DD`. `ALTER COLUMN … TYPE` that converts values does the same under the table's own name: `FT.CREATE` of the new names' index with the new attributes, pipelined `HGETALL` of every row the old index lists and `HSET` of each converted row to its new key, one transaction to switch the metadata, then `FT.DROPINDEX <old> DD`; it also scans the views (`SMEMBERS` and `GET` of each one) to find those that read the column |
| Views | `CREATE VIEW` plans the body without running it, which checks its tables, columns and function calls. Single-table views without GROUP BY/aggregates/window functions/LIMIT are expanded in place: the outer query's filters are rewritten over the base table and run in its index. Other views are computed once per query, like a derived table |
| Joins | Each table's own WHERE/ON filters run in its index (except on the NULL-supplying side of an outer join). Inner joins are reordered to start from the table with the fewest matches (counted by the index). Equality conditions drive a hash join; when the next table's key is indexed and there are ≤ 1,000 distinct keys, only matching rows are fetched with an index union. The joined rows are then grouped/sorted in memory. `NATURAL JOIN` is an equality join on the common columns, like `USING`. An item whose `ON` can never be true (`ON false`), or that has no rows, isn't read when that settles the result: an inner join is then empty and reads no item, a `LEFT JOIN` keeps the left rows with NULLs for the item, and a `RIGHT JOIN` returns the item's rows without reading the items before it. A FROM-list item after a comma that has a `RIGHT` or `FULL` join is joined on its own, then cross joined with the rows of the items before the comma (see "Commas"), and `WHERE` conditions that read both are applied after that. An `ON` condition with a correlated subquery waits until every item it may read is joined (up to v0.0.12, in an inner join of three or more items it could run before, and `from a join b on true join c on exists (select … where … = c.x)` returned no rows) |
| `UPDATE … FROM`, `DELETE … USING`, `MERGE` | The target is joined with the other items as above: its own filters (in WHERE, or in MERGE's ON) run in its index, and an equality on an indexed target column is an index lookup join, also through a no-op cast like dbt's `s.id::text = t.id::text`. `MERGE` is source `LEFT JOIN` target, or `FULL JOIN` with `WHEN NOT MATCHED BY SOURCE` clauses (which need every target row); `ON FALSE` reads the target only for those. Changes are then written by row key with pipelined `HSET`/`HDEL`/`DEL`, and new rows like `INSERT` does |
| Anything the index can't answer exactly | Evaluated by the driver on rows fetched from the HASHes |

### Streamed results

A query that reads one table through its index, row by row, is **streamed**:
its rows are read from the `FT.AGGREGATE` cursor a page at a time as the
result is read, so the driver holds about one batch of them, whatever the
table's size. Other results are built whole before `ExecuteQuery` returns.

- **What streams:**
  - a single `SELECT` (not a script), run without parameters or with one
    row of them;
  - on a table, not a view, CTE, derived table or join;
  - matched through the index, rather than by row id (`WHERE __rowid = N`);
  - without aggregates, `GROUP BY`, `DISTINCT`, set operations, window
    functions or correlated subqueries;
  - in insertion order, or ordered by indexed columns the index sorts
    exactly. An `ORDER BY` the driver sorts itself (an expression,
    `NULLS FIRST`, …) reads the rows whole. Rows that tie on a large
    number in the index are sorted again as they stream (see "Ties in the
    sort vector" above).

  `LIMIT` and `OFFSET` that can't run in the index (because the driver
  re-checks part of the WHERE clause) are applied as the rows stream, and
  the cursor is deleted once the limit is reached.
- **Row count.** `ExecuteQuery` reads the first batch before it returns, so
  a result that fits in it is returned whole, with its row count, as
  before. A longer one is a stream, whose row count is -1.
- **Errors.** One in the first batch comes from `ExecuteQuery`. A later one
  (division by zero on row 100,000, say) ends the stream: `Next` returns
  false, and `Err` reports it.
- **Cursors.** A stream keeps its `FT.AGGREGATE` cursor open until it is
  read to the end, released, or its connection is closed (reading it then
  fails). Cancelling the context passed to `ExecuteQuery` (in Go) also
  deletes it, as does cancelling the statement (`AdbcStatementCancel`;
  `cursor.adbc_cancel()` in Python), from any thread: reading the stream
  then fails with CANCELLED (see
  [Cancelling a statement](#cancelling-a-statement)). A read under
  way first waits for the cursor page it asked for. Redis limits cursors:
  - It deletes one that isn't read for `search-cursor-max-idle` (300
    seconds by default). Reading the stream then fails, with a message
    naming `adbc.redis.stream_results`.
  - An index has at most `search-index-cursor-limit` open cursors (128 by
    default), across every client. A query on a table with that many open
    streams fails until one ends.
- **Options.** `adbc.redis.stream_results=false` (database or statement)
  turns streaming off. `adbc.redis.stream_batch_rows` sets the rows per
  batch (65,536 by default).

Pushed down into the index: numeric range/equality predicates on indexed
columns (`@c:[lo hi]`), string equality on indexed columns (`@c:{value}`,
re-checked by the driver once the column holds a value the index trims,
splits or cuts; see "Strings in the index" above), `ORDER BY` on indexed
columns, `LIMIT/OFFSET`, and aggregates. A constant
that doesn't fit the column's type exactly (`int_col > 1.5`, `numeric_col =
1.249`, `date_col < TIMESTAMP '… 12:00:00'`) is pushed as an inclusive bound
at its rounded value and re-checked by the driver, as is a `NaN` or
`Infinity` compared with a float column (see "NaNs in the index" above). A
`TIMESTAMP WITH TIME ZONE` column compared with a local time (text without
an offset, a `TIMESTAMP` or a `DATE`) is pushed as that local time's
instant in the session time zone. A `TIMESTAMP` or `DATE` column compared
with a timestamp with time zone is pushed as the range of local times that
can be that instant (in a DST gap, 02:30 is the same instant as 03:30) and
re-checked.

Row values always come from the HASHes as stored, never from the index sort
vectors: `LOAD @c` on a SORTABLE numeric attribute returns the sort vector's
double printed with 12 significant digits, which would round 64-bit integers,
timestamps, decimals and doubles. So the driver names the columns it needs
(`LOAD n @__key @__rowid @c …`) only when each one is a string (unless the
column may hold a NUL byte, at which the sort vector stops), a 16/32-bit
integer, boolean, date or time, or not indexed. When a managed-table scan
needs at most half the table's columns, it loads the exact-safe fields and
keys through the cursor and reads the remaining fields from the HASHes
with pipelined `HMGET` (at most 1,000 commands per batch). This preserves
exact BIGINTs without transferring every column of a wide row. Wider
projections and adopted tables use `LOAD *`, which returns the HASH fields
unchanged. The two-stage read can observe concurrent changes between the
cursor and `HMGET`; queries do not provide a snapshot. Sorts run on aliases
(`LOAD @c AS __sort0 SORTBY @__sort0`) so that on a cluster the coordinator
merges the shards' results on the numeric sort values rather than on the
strings `LOAD *` returns. Rows whose sort values tie as doubles are sorted
again by the driver (see "Ties in the sort vector" above).

The [narrow-scan profile](benchmarks/narrow-scan/report.md) compares both
paths on the official 100,000-row ClickBench fixture, including transferred
bytes, allocations and exact aggregate query timings.

### Aggregate pushdown

The `adbc.redis.aggregate_pushdown` option controls where aggregates run:

- `exact` (default): pushes COUNT, plus SUM/AVG/MIN/MAX over 16/32-bit
  integer, boolean and date columns, which RediSearch returns exactly, and
  BOOL_OR/BOOL_AND/EVERY over boolean columns. Other aggregates are computed
  by the driver, except for the exact COUNT DISTINCT strategy below.
- `all`: also pushes floating-point, decimal and 64-bit aggregates, and
  groups on such columns (see below). RediSearch adds doubles in its own
  order (on a cluster, each shard's rows, then the shards' sums) and
  reports the result with 12 significant digits; `AVG` is that sum divided
  by the count. That is the exact sum when it has at most 12 significant
  digits, such as amounts with two decimals, and RediSearch's rounding
  errors stay below the 12th digit; otherwise it differs from it after the
  12th digit (`SUM` of 1/3 and 1/7 is 0.47619047619, not
  0.47619047619047616), and sums that cancel can lose their small values
  (1e16, 1, -1e16, 3, 1e16, 0.5, -1e16 came back as 4, not 4.5). A float
  column that may hold a NaN is grouped and reduced by the driver (see
  "NaNs in the index" above).
  - **Groups are exact.** The index groups (`GROUP BY`, `SELECT
    DISTINCT`) on its doubles too, and replies with a group's value in full
    if it is an integer and with 12 significant digits otherwise. The
    driver takes the index's groups only if each one's value is exactly a
    value of the column, and otherwise groups the rows itself, after that
    `FT.AGGREGATE`. That is a `BIGINT`, `TIME` or `TIMESTAMP` value below
    2^53 in its unit (a `TIMESTAMP` from 1685 to 2255), a `NUMERIC(p,s)`
    value of at most 12 significant digits (any for p ≤ 12, otherwise
    those below 10^(12-s): 1,000 for `NUMERIC`, which is `NUMERIC(38,9)`),
    and a `REAL` below 2^63 or infinite. `DOUBLE PRECISION` columns
    (5.0000000000001 is replied as 5) and nanosecond `TIMESTAMP`s (past
    2^53 from April 1970) aren't grouped in the index at all. Up to v0.0.12
    the driver took the index's groups as they came: 9007199254740992 and
    9007199254740993 were one group, as were `TIMESTAMP(9)` values of 2024
    up to 256 ns apart; `SELECT DISTINCT` returned 0.3 twice for 0.3 and
    0.30000000000000004, and 9999-12-31 23:59:59.999999 as 10000-01-01.
  - **So are `MIN` and `MAX` of a `BIGINT`, `TIME` or `TIMESTAMP`**, which
    come from the index on the same condition, **and `SUM`s of integers.**
    RediSearch's sum of integers is exact while the count times the
    largest magnitude is below 2^53: for a `BIGINT`, the driver asks the
    index for the group's `MIN` and `MAX` too; for an `INTEGER` (in the
    `exact` mode too), up to 4 million values. The driver computes the
    others. Up to v0.0.12, `MIN` of -9007199254740993 was
    -9007199254740992, and the `SUM` of 9007199254740992,
    -9007199254740993 and 5 was 5. `MIN` and `MAX` of decimals and doubles
    keep the index's 12 significant digits.
- `none`: always aggregates in the driver.

The other modes compute `SUM` and `AVG` of doubles exactly (see
Aggregates, below), so their results don't depend on the mode, the order of
the rows, or the server being standalone or a cluster.

Driver-side aggregates over indexed tables consume one cursor page at a
time, updating their accumulators before fetching the next page. Each group
retains only the representative fields used after reduction. Eligible
`ORDER BY … LIMIT … OFFSET …` queries select the best `LIMIT + OFFSET`
projected groups with a bounded heap, preserving stable ties; queries with
windows or DISTINCT keep their existing ordering path. Exact distinct sets,
group state and ordered-set inputs still grow with cardinality. In-memory
relations and the shared input for grouping sets retain their existing
materialization. The [aggregation profile](benchmarks/aggregate-pages/report.md)
reports latency and memory across input sizes and group cardinalities.

In `exact` and `all` modes, queries whose aggregates are all
`COUNT(DISTINCT col[, …])` can deduplicate complete tuples in Redis and
count them in the driver, without transferring every source row or retaining
client distinct sets. Eligible grouping and argument columns are indexed
strings, integers (including BIGINT), booleans, dates, times and timestamps.
Safe sorting vectors need no load. For BIGINT, timestamps and NUL-containing
strings, `LOAD *` **before** `GROUPBY` uses the original HASH strings; named
loads still use rounded or truncated index values. This preserves values
above 2^53 and embedded NUL bytes without adding stored fields or indexes.
Complete tuples are merged across shards before counting, and any tuple
with a NULL argument is excluded. Redis GROUPBY uses a hash as its identity;
per-column TOLIST sets compare complete values, and MIN/MAX of presence
flags check NULL/value mixtures. A hash group containing different tuples
triggers the exact row-based fallback. For NUL-containing columns, the
strategy also verifies Search 8.6 or newer with `MODULE LIST`; older versions
or an ACL that denies that command retain the exact row path.

Aggregate filters use exact index predicates or pure comparisons over tuple
columns. The latter also recheck normalized string predicates, such as
`SearchPhrase <> ''`. A separate group query preserves zero-count groups
when every aggregate is filtered. Equivalent aggregate calls share a
pipeline; different calls use separate pipelines. This does not provide a
snapshot across concurrent writes. Adopted tables, unindexed columns,
virtual defaults, noncanonical stored values, expressions, mixed aggregates
and other predicates retain the exact row-based path. `none` disables tuple
deduplication. The [distinct benchmark](benchmarks/exact-distinct/report.md)
records wire bytes, latency, driver allocations and server memory. Raw
loading moves work and tuple state to Redis; high cardinality can transfer
as many tuples as input rows, and there is no spill or new memory limit.

Other `DISTINCT` aggregates, filters outside that scope and other aggregates
run in the driver. RediSearch has reducers that look like some of them, but none
gives SQL's answer: `STDDEV` loses precision on values far from zero (150
values near 10⁹ came back wrong in the 8th significant digit, on a cluster
too) and is 0 instead of NULL for a single value, `QUANTILE` is an
approximate streaming estimate, `TOLIST` de-duplicates in no particular
order, and `FIRST_VALUE` returns NULL when the group's first row lacks the
field, even if later rows have it.

## Supported SQL

### Schema objects and DDL

- `CREATE TABLE [IF NOT EXISTS] t (col TYPE [column_constraint …] [NOINDEX] [COMMENT 'text'], …
  [, table_constraint, …]) [COMMENT [=] 'text']`, where a column constraint
  is `[CONSTRAINT name] {NOT NULL | NULL | CHECK (expr) | DEFAULT expr |
  PRIMARY KEY | UNIQUE | REFERENCES …}` and a table constraint is
  `[CONSTRAINT name] {CHECK (expr) | PRIMARY KEY (cols) | UNIQUE (cols) |
  FOREIGN KEY (cols) REFERENCES …}` (see Constraints below; the `COMMENT`
  clauses, MySQL / Snowflake style, set the same comments as `COMMENT ON`),
  `CREATE TABLE [IF NOT EXISTS] t AS SELECT …` (column names and types come
  from the query; every indexable column is indexed),
  `DROP TABLE [IF EXISTS] t [CASCADE | RESTRICT]`,
  `CREATE SCHEMA [IF NOT EXISTS] s`,
  `CREATE [OR REPLACE] VIEW [IF NOT EXISTS] v [(cols)] AS SELECT …`,
  `DROP VIEW [IF EXISTS] v [CASCADE | RESTRICT]`,
  `ALTER VIEW [IF EXISTS] v RENAME TO w` (`ALTER TABLE v RENAME TO w` also
  renames a view, as in Postgres; other `ALTER TABLE` actions on a view are
  rejected). Renaming changes metadata only; views that read the object by
  its old name stop working, as they do after a table rename
- `CREATE {TEMP | TEMPORARY} TABLE [IF NOT EXISTS] t (…)`,
  `CREATE TEMP TABLE [IF NOT EXISTS] t AS [(]SELECT …[)]`,
  `CREATE [OR REPLACE] TEMP VIEW [IF NOT EXISTS] v [(cols)] AS SELECT …`, and
  bulk ingest with `adbc.ingest.temporary`. Temporary tables and views work
  like permanent ones in every statement (joins with permanent tables, CTEs,
  `ALTER`, `TRUNCATE`, `DROP`, …), with these differences:
  - **Scope:** only the ADBC connection that created them sees them, so two
    connections can each have a temporary table with the same name.
  - **Lifetime:** they are dropped when the connection is closed
    (`AdbcConnectionRelease`). If the process exits without closing it, the
    next connection to open after the 2-minute heartbeat TTL has expired
    drops them. A connection that can't reach Redis for longer than that may
    lose its temporary objects.
  - **Names:** an unqualified name means a temporary table or view first, so
    it shadows a permanent one of the same name, as in Postgres. `schema.t`
    always means a permanent table, and `pg_temp.t` the temporary one
    (`CREATE TABLE pg_temp.t` creates a temporary table). Without `TEMP`,
    `CREATE TABLE` / `CREATE VIEW` create permanent objects, and bulk ingest
    without `adbc.ingest.temporary` targets the permanent table. The schema
    names `pg_temp` and `pg_temp_<digits>` are reserved.
  - **Metadata:** `GetObjects` and `information_schema` list them under the
    schema `pg_temp`, only for the connection that owns them and only while
    it has any. `information_schema.tables` reports temporary tables as
    `LOCAL TEMPORARY`, as Postgres does.
  - **Views:** a temporary view may read temporary and permanent tables. Its
    unqualified names resolve like the connection's own statements:
    temporary objects first, then the schema that was current when the view
    was created. A permanent view can't refer to a temporary table or view
    (`CREATE VIEW` fails; use `CREATE TEMP VIEW`), and temporary objects
    never shadow the names inside a permanent view. A view keeps only its
    SQL text, so a temporary table created later with the name of a
    permanent table that a temporary view reads takes that table's place in
    the view.
- `DROP SCHEMA [IF EXISTS] s [CASCADE | RESTRICT]`: `RESTRICT` (the default)
  refuses a schema that still has tables or views; `CASCADE` drops its views,
  then its tables (indexes and rows), then the schema. Dependencies between
  tables and views aren't tracked, so `CASCADE` / `RESTRICT` on `DROP TABLE`,
  `DROP VIEW` and `DROP COLUMN` are accepted but don't drop or check views
  that read the object (such a view fails when queried, as it does after a
  plain `DROP`)
- `TRUNCATE [TABLE] [ONLY] t [, …] [RESTART IDENTITY | CONTINUE IDENTITY]
  [CASCADE | RESTRICT]` removes every row and keeps the table: its columns,
  defaults, `CHECK` constraints and comments. Row ids (`__rowid`) continue
  unless `RESTART IDENTITY` is given; the driver has no identity or serial
  columns, so the row id counter is all it restarts. There are no foreign
  keys, so `CASCADE` and `RESTRICT` do the same:
  - **New keys:** the table moves to a new key prefix and index,
    `<schema>:<table>~N:` and `idx:<schema>:<table>~N` with a new N (see
    "Key prefixes are never reused" in the architecture section), as if it
    were dropped and created again. Then `FT.DROPINDEX … DD` on the old
    index deletes the old rows. `information_schema.tables.key_prefix` and
    `index_name` show the new names.
  - **All or none:** the tables of one statement switch to their new names
    in one transaction, so either every table is emptied or none is. A
    table named twice is emptied once.
  - **Concurrent writes:** there are no locks, so another connection may
    still be writing to the table (in Postgres, `TRUNCATE` would wait for
    it). Such a statement fails as after `DROP TABLE` ("… truncated or
    dropped, while this statement was writing to it …") and deletes what it
    wrote, so the table is empty afterwards. That includes `UPDATE` and
    `MERGE`, whose changes after the `TRUNCATE` would leave partial copies
    of the rows they set under the old prefix. A `DELETE` (or `MERGE …
    DELETE`) leaves nothing there: one that ran for 100 ms or more fails
    all the same (it can't tell a `TRUNCATE` from a re-keying rename, which
    copied the rows it hadn't deleted yet), and a faster one succeeds, as
    if it had run first. A small write that doesn't check may succeed, with
    its rows left under the old prefix, where no table reads them. Up to
    v0.0.7 the rows such a statement wrote after the `TRUNCATE` stayed in
    the table, and `RESTART IDENTITY` could lose them (see "Key prefixes"
    in the architecture section).
  - **Concurrent reads and `ALTER`s:** a statement that reads the table for
    100 ms or more across a `TRUNCATE` fails ("try again"), as across a
    `DROP TABLE`. An `ALTER TABLE … ADD COLUMN` that commits while a
    `TRUNCATE` runs is added to the new index too; one that had changed
    the old index and commits after it fails ("changed concurrently; try
    again")
- `ALTER TABLE [IF EXISTS] t` with `RENAME TO u`, with
  `RENAME [COLUMN] a TO b`, or with one or more of these, separated by commas:
  `ADD [COLUMN] [IF NOT EXISTS] c TYPE [column_constraint …] [NOINDEX] [COMMENT 'text']`,
  `DROP [COLUMN] [IF EXISTS] c [CASCADE | RESTRICT]`,
  `ALTER [COLUMN] c {SET DEFAULT expr | DROP DEFAULT | SET NOT NULL | DROP NOT NULL}`,
  `ALTER [COLUMN] c [SET DATA] TYPE type [USING expr]`,
  `ADD table_constraint`,
  `DROP CONSTRAINT [IF EXISTS] name [CASCADE | RESTRICT]`. All of them only
  change metadata, so they take the same time at any table size, except
  `RENAME TO` with `adbc.redis.rename_rekey` and `ALTER COLUMN … TYPE`,
  which rewrite every row, and adding a `CHECK`, a `NOT NULL` or a
  `PRIMARY KEY`, which reads every row first:
  - **Several actions** (dbt's `on_schema_change` and snapshots send
    `alter table t add column "a" …, add column "b" …, drop column "c"`) are
    one change of the table's metadata, made in one `WATCH`/`MULTI`, so
    other statements see the table before all of them or after all of
    them. They run in Postgres's order, each seeing what the ones before it
    did: first the drops (`DROP COLUMN`, `DROP CONSTRAINT`, `DROP DEFAULT`
    and `DROP NOT NULL`), then `ALTER COLUMN … TYPE` (and the `CHECK`s that
    read the columns it changes, bound again), then `ADD COLUMN`, then `SET
    NOT NULL`, then the primary keys (`ADD PRIMARY KEY` and `ADD COLUMN …
    PRIMARY KEY`), then `SET DEFAULT`, the new columns' `CHECK`s and `ADD
    CONSTRAINT … CHECK`, each group in the order written. So `ADD
    CONSTRAINT c CHECK (x > 0), ADD COLUMN x … DEFAULT 1` checks the rows
    with `x`'s default, `DROP COLUMN a, ADD COLUMN a …` gives a new, empty
    column (also written the other way round), `ALTER COLUMN a SET DEFAULT
    1, ADD COLUMN a …` gives the new column a default, `ADD PRIMARY KEY (b),
    ADD COLUMN b …` makes the new column the key, and `ADD PRIMARY KEY (b),
    DROP CONSTRAINT t_pkey` replaces the key. An action that runs earlier
    can't name what the
    statement adds: `ADD COLUMN a …, DROP COLUMN a` and `ADD COLUMN a …
    DEFAULT 9, ALTER COLUMN a DROP DEFAULT` fail with `column "a" of
    relation "t" does not exist`, as in Postgres. Unnamed constraints are
    named in that order too: in `ADD CHECK (v > 0), ADD COLUMN b … CHECK (v
    > 1)`, the column's is `t_v_check` and the other `t_v_check1`, and in
    `ADD CHECK (b > 0), ADD CONSTRAINT t_b_check PRIMARY KEY (b)` the key
    takes `t_b_check` and the `CHECK` is `t_b_check1`. Of several
    errors, the one reported is the first in that order. A table can't be
    left without columns (Postgres allows it), but `DROP COLUMN a, ADD
    COLUMN b …` works on a table whose only column is `a`.
    - **Upgrading from v0.0.12 and earlier:** the actions ran in the order
      written, so `ADD COLUMN a …, DROP COLUMN a` added nothing, `ADD COLUMN
      a … DEFAULT 9, ALTER COLUMN a DROP DEFAULT` left `a` without a default
      (for the rows inserted later), and a `CHECK` on a column added later
      in the list was an error. `ADD COLUMN … NOT NULL` without a non-NULL
      `DEFAULT` was an error also on a table without rows. In `ADD COLUMN`,
      `DROP COLUMN` and `ALTER COLUMN`, a column that already exists or
      doesn't was `column "a" already exists in table "t"` or `column "a"
      does not exist in table "t"`.
  - **All or nothing:** if any action fails (a column that exists or
    doesn't, a `DEFAULT` that doesn't convert, a `CHECK` that some row
    fails, …), none is applied: the metadata, the search index and the
    rows stay as they were. Every definition is checked first, then the
    rows against the new `CHECK`s (one scan for all of them), then the new
    indexed columns are added to the index (one `FT.ALTER`, which adds all
    of them or none), and then the metadata is written. No row is
    rewritten before that: the only row writes are the removal of dropped
    columns' fields afterwards, in the background, which is recorded in
    the metadata and resumed by the next connection if it stops part-way
    (the table is already correct then: the fields are never read or
    reused). If writing the metadata fails (a concurrent change, "try
    again", or a lost connection), the index keeps attributes that no
    column uses, which is harmless, and a later `ADD COLUMN` doesn't reuse
    their names. A statement with an `ALTER COLUMN … TYPE` that converts
    values writes new rows instead, under new keys, and switches to them
    in one transaction once every row is converted and checked (see
    below), so it is all or nothing too.
  - **`RENAME TO` and `RENAME COLUMN`** take no list, as in Postgres:
    `ALTER TABLE t RENAME TO u, ADD …` is `syntax error at or near ","`,
    and `ALTER TABLE t ADD …, RENAME …` is `syntax error at or near
    "RENAME"`. On a view, only `RENAME TO` is accepted.
  - `RENAME TO` keeps the table's row keys and index (fixed when the table
    was created), so no row is touched. A table that dbt builds as
    `t__dbt_tmp` and renames to `t` keeps the keys `public:t__dbt_tmp:<rowid>`
    and the index `idx:public:t__dbt_tmp` (or `t__dbt_tmp~N`: names are never
    reused, so every build after the first takes a new N).
  - **With a concurrent `DROP TABLE`:** there are no locks, so
    `DROP TABLE t` and `ALTER TABLE t RENAME TO u` may overlap. In Postgres
    one of them waits for the other: either the `DROP` drops the table and
    the rename fails, or the rename comes first and the `DROP` fails. Here
    a rename that commits before the `DROP` reads the table makes the
    `DROP` fail (`table "public"."t" does not exist`), and a `DROP` that
    commits before the rename makes the rename fail the same way. A rename
    that commits while the `DROP` runs is too late to save the table: the
    `DROP` drops the index, which deletes the rows, and the renamed table
    keeps that index. So the `DROP` drops the table under its new name,
    `u`, and both statements succeed. A table created as `t` meanwhile is
    another table, and stays. Up to v0.0.12 the `DROP` failed ("does not
    exist"), did nothing under `IF EXISTS`, or dropped that new `t`
    instead, and `u` was left without its index or rows, its key prefix and
    index name still reserved.
  - With the option `adbc.redis.rename_rekey` set to `true` (on the
    database, e.g. in dbt's `db_kwargs`, or on the connection), `RENAME TO`
    also moves the rows and the index to the new name's: `public:t:<rowid>`
    and `idx:public:t`, or `~N` if a table has had those before. In dbt's
    swap the previous build's `t` gives them up on every build, so from the
    second build on the keys are `public:t~N:`, with a new N each time. This
    reads and writes every row, so it takes time proportional to the number
    of rows (and about 100 ms more), and the table takes twice its memory
    until the copy is done (if Redis runs out, the rename fails and is
    rolled back). Every rename does it, including dbt's renames to
    `__dbt_backup`. The rows are copied with `DUMP` / `RESTORE`, every field
    included, then the metadata is switched in one transaction and the old
    index is dropped with `DD`, which deletes the old rows. Views are still
    renamed in the metadata only. While the rows move:
    - Readers see the table, complete, under its old name until the switch
      and under the new one after it. A statement that read the table's
      metadata during the move and was still running at the switch, or one
      that ran for 100 ms or more while the rows moved away, fails ("try
      again") rather than returning rows that are gone or now belong to
      another table.
    - Statements that would change the table are refused ("is being
      renamed"): `INSERT`, `UPDATE`, `DELETE`, `MERGE`, `TRUNCATE`, bulk
      ingest that appends, and `ALTER TABLE`. An `ALTER TABLE … ADD`, `DROP`
      or `RENAME COLUMN` that had already started is taken into account when
      the metadata is switched. `DROP TABLE` (and so bulk ingest in
      `replace` mode) is not refused: if it reaches the table before the
      switch, the rename fails. If the switch comes first, the `DROP`
      fails ("does not exist"), and the renamed table keeps its rows,
      which were copied to its new keys: unlike a plain rename, this one
      isn't followed.
    - A write statement that read the table's metadata before the move
      started may still be running, so the copy waits 100 ms first. A write
      statement that took longer than that, from reading the metadata to its
      last write, checks afterwards whether the table was being moved (or
      was moved, truncated or dropped), and if so fails with "some of its
      changes may be lost or have gone to another table": like a write that
      fails part-way, some of them may have been applied. Faster statements
      don't pay for the check, unless they write more than 1,000 rows (see
      "Key prefixes" in the architecture section).
    - If the renaming process exits, the table is unchanged under its old
      name and refuses changes until the move's 30-second lease has
      expired. Until then the refusal says to try again when the rename
      has finished, and that a rename whose connection has gone away is
      rolled back once its lease has expired. Then the move is rolled back
      (its copies, its index and the names it reserved are removed), before
      anything else uses the table:
      - **A connection that opens** rolls it back before it is open, even
        one that is closed straight away, as dbt's metadata connections
        are. Connections that open while another one rolls it back wait
        for that (up to 30 seconds).
      - **A connection that was already open** rolls it back at its next
        statement that would change the table, or its next re-keying
        rename, and that statement then goes on.
      - **If the rollback fails** (for example, the user lacks
        `FT.DROPINDEX`), the connection gives it up at once, and the
        statement fails with the reason and says that the next connection
        tries again.

      If it had already switched the metadata, the rename stands, and the
      same connections remove the old rows instead. A renaming connection
      that can't renew its lease for 15 seconds stops before its next step
      and fails; another connection then rolls the move back.
    - If the renaming connection is closed (`AdbcConnectionRelease`) while
      the rename runs on another thread, the rename stops as if cancelled.
      Close returns once it is rolled back, or finished if it had switched
      the metadata. No lease is left, so other connections change the table
      at once. The rename fails with "the connection was closed while the
      table was being renamed", or with CANCELLED if its statement was
      released first (Python's `close()` closes the cursors first). Close
      waits up to 15 seconds. A rename still running then gives its lease
      up, and the next connection rolls it back at once. Up to v0.0.12,
      Close left the lease in place, so the table refused changes for 30
      seconds, and the process could crash.
  - `RENAME COLUMN` keeps the column's HASH field; only its SQL name changes.
  - `ADD COLUMN` adds the column to the index with `FT.ALTER`; existing rows
    read it as NULL, or as its `DEFAULT`, which is computed once (so
    `DEFAULT CURRENT_TIMESTAMP` gives them all the time of the `ALTER`) and
    recorded as the column's missing value rather than written to them (see
    the architecture section). A volatile default (`RANDOM()`) is not
    supported here. With `NOT NULL` and no `DEFAULT` (or a NULL one), the
    existing rows would read NULL, so as in Postgres it works only on a
    table without rows (otherwise `column "c" of relation "t" contains null
    values`).
  - `DROP COLUMN` hides the column immediately and removes its field from
    existing rows in the background (resumed by the next connection if the
    process exits first). A dropped column's values never reappear, even if
    a column with the same name is added later.
  - `ALTER COLUMN … SET DEFAULT` / `DROP DEFAULT` only change what rows
    inserted later get; existing rows keep their values.
  - `ALTER COLUMN … SET NOT NULL` checks every row first, reading the
    column as queries do (one added with a `DEFAULT` reads it in the rows
    that existed then), and fails with Postgres's `column "c" of relation
    "t" contains null values` if one is NULL. Each row is checked against
    `NOT NULL` before the statement's new `CHECK`s, as in Postgres. `DROP
    NOT NULL` only changes the metadata. Either does nothing to a column
    that already is so. `information_schema.columns.is_nullable`,
    `GetObjects` and `GetTableSchema` show the change. `DROP NOT NULL` on a
    column of the primary key fails with Postgres's `column "id" is in a
    primary key`, unless the statement drops the key first (`DROP
    CONSTRAINT t_pkey, ALTER COLUMN id DROP NOT NULL`; both are drops, which
    run in the order written).
  - **Concurrent writes:** there are no locks, so other connections may
    write while the rows are checked for a new `NOT NULL` or `CHECK` (in
    Postgres they would wait). The rows they insert meanwhile are checked
    too, when the change commits (which tries again while more keep coming,
    up to 20 times, then fails with "try again"). A statement that read the
    table before a new `NOT NULL` and inserts after it fails on a NULL in
    the column, or with "try again" if the column was added without a
    default. Rows still being written when the change commits, rows updated
    meanwhile, and rows inserted after a new `CHECK` by a statement that
    read the table before it aren't checked.
  - `ALTER COLUMN c [SET DATA] TYPE type [USING expr]` converts the
    column's values, as in Postgres:
    - **Without `USING`,** the column's value is converted as an `INSERT`
      converts it, which Postgres allows for these types: a number to any
      number type, `DATE` to `TIMESTAMP` (either kind), a `TIMESTAMP` to
      `DATE`, `TIME` or the other `TIMESTAMP`, `TIME` and `INTERVAL` to
      each other, `TIME` and `TIME WITH TIME ZONE` to each other, a
      `TIMESTAMP WITH TIME ZONE` to `TIME WITH TIME ZONE`, and any type to a
      string type. Otherwise it fails with
      Postgres's `column "c" cannot be cast automatically to type integer;
      you might need to specify "USING c::integer"`. A string that is too
      long for a `VARCHAR(n)` or `CHAR(n)` is an error (`value too long for
      type character varying(2)`), unless the rest is spaces; `USING
      c::varchar(2)` cuts it.
    - **`USING expr`** converts an expression over the row instead, which
      may read any of the table's columns and call functions (`RANDOM()`
      gives each row its own value), but not use subqueries, parameters,
      aggregates or window functions (`cannot use subquery in transform
      expression`, …). Its result is converted as above (`result of USING
      clause for column "c" cannot be cast automatically to type integer`
      otherwise; a quoted string literal or `NULL` converts to any type, a
      cast of one doesn't). It reads the row as it was before the
      statement: `ALTER COLUMN a TYPE text USING b::text, ALTER COLUMN b
      TYPE int USING a::int` swaps the values, and `DROP COLUMN b, ALTER
      COLUMN a TYPE int USING b` reads `b`.
    - **The rest of the column stays:** its name, `NOT NULL` (a row that
      the conversion makes NULL fails with `column "c" of relation "t"
      contains null values`), primary key, comment and `DEFAULT`. The
      `DEFAULT` must
      convert too (`default for column "c" cannot be cast automatically to
      type integer`; drop it first, in the same statement, and set a new one
      after), and is checked as a new one is. The `CHECK` constraints that
      read the column are bound again for the new type (one that no longer
      works fails the statement, `argument of CHECK must be type boolean,
      not type integer`), and every converted row must pass them.
    - **Refused, as in Postgres:** a column that a view reads (`cannot alter
      type of a column used by a view or rule; rule _RETURN on view v
      depends on column "c"`), even to the same type, and a column whose
      type changes twice in one statement (`cannot alter type of column "c"
      twice`). Dependencies aren't recorded, so the views of every schema,
      and the connection's temporary views, whose text names the table are
      planned to find the columns they read. Other connections' temporary
      views aren't checked. `COLLATE` isn't supported (strings compare by
      their bytes, as with Postgres's `"C"` collation).
    - **Only the metadata changes** when the values stay as they are: the
      same type without `USING`, or a `VARCHAR` that gets longer or loses
      its length (`TEXT`). Postgres doesn't rewrite the table for these
      either.
    - **Otherwise every row is rewritten,** as in Postgres, and like
      `RENAME TO` with `adbc.redis.rename_rekey` (above), to new keys: the
      table takes new names (`<schema>:<table>~N:` and its index), and
      every row the old index lists is read (`HGETALL`), converted, checked
      against `NOT NULL` and the `CHECK`s (also those the statement adds),
      and written to its new key (`HSET`), pipelined, a cursor page at a
      time. The new index has the column's new attribute (`NUMERIC` for
      numbers, dates and times, `TAG` for strings, none for `BYTEA`,
      `INTERVAL` and `TIME WITH TIME ZONE`), so indexed queries on the
      column read the new type. Before switching, the old and new indexes'
      row counts are compared. If background dropped-column cleanup changed
      the old index during a cursor scan, missing rows are rescanned (up to
      three scans); already copied rows aren't converted again. A persistent
      mismatch fails and rolls back rather than committing missing rows. One
      transaction then switches the metadata to the new names, with every
      action of the statement, and the old index is dropped with its rows
      (`DD`). The new rows hold only the table's columns: the fields of
      dropped columns aren't copied (and no cleanup is left), and a column
      added in the same statement with a `DEFAULT`, or an altered column
      that had a missing value, gets its value written into every row. This
      takes time proportional to the number of rows (and 100 ms more), and
      the table takes twice its memory until the old rows are dropped.
    - **All or nothing:** a value that doesn't convert, a row that fails a
      constraint, a cancel (`AdbcStatementCancel`, CANCELLED), the
      connection's close or a lost connection rolls the change back: the
      new index is dropped with its rows, and the table is as it was.
    - **Concurrent statements** are those of a re-keying rename: while the
      rows are rewritten, writes are refused ("is being altered: ALTER
      COLUMN … TYPE is rewriting its rows to new keys; try again when that
      has finished"), as are other `ALTER`s and `TRUNCATE`; readers see the
      table as it was until the switch; a statement that read the table
      before and writes, or reads for 100 ms or more, across the rewrite
      fails ("was renamed with its rows moved to new keys, rewritten by
      ALTER COLUMN … TYPE, truncated or dropped, while this statement was
      …"). An `ALTER` (or `COMMENT ON`) that had already started and
      changes the table meanwhile makes the rewrite fail ("was changed
      while ALTER COLUMN … TYPE rewrote its rows; try again"), and so does
      a `DROP TABLE`. If the connection goes away, or can't renew its
      lease, the next connection to open, or the next statement that
      changes the table, rolls the rewrite back, as for a rename. It needs
      no commands beyond the basic ones (see [ACL users](#acl-users)).
    - **Not supported:** conversions that the driver's `CAST` doesn't make
      either, such as `TIME` to `INTERVAL` (`cannot convert time value …`),
      and tables adopted with `redis-adbc adopt`, whose HASHes are the
      application's. Values convert as the driver's `CAST` converts them,
      which differs from Postgres in a few places: a `DOUBLE PRECISION`
      half rounds away from zero to an integer (Postgres rounds to even),
      `BYTEA` keeps its text as written, and a conversion error has the
      driver's message (`invalid decimal "abc"`, where Postgres says
      `invalid input syntax for type integer: "abc"`). As for the other
      defaults, a `DEFAULT` that the new type can't hold is an error at the
      `ALTER` (Postgres fails when a row gets it).
    - **Upgrading from v0.0.12 and earlier:** `ALTER COLUMN … TYPE` was
      `unsupported ALTER COLUMN action near "TYPE"`. The error of a
      statement whose table moved away while it was writing or reading now
      names `ALTER COLUMN … TYPE` among the causes.
  - `ADD COLUMN … CHECK (…)` and `ADD [CONSTRAINT name] CHECK (…)` check
    every existing row first (see Constraints below). `ADD [CONSTRAINT
    name] PRIMARY KEY (cols)` and `ADD COLUMN … PRIMARY KEY` make the key's
    columns `NOT NULL`, checking every row first as `SET NOT NULL` does
    (see "Primary keys" under Constraints below). `DROP CONSTRAINT` removes
    a `CHECK` or the primary key; `UNIQUE` and `FOREIGN KEY` constraints
    aren't kept, so it can't name them.
- `COMMENT ON {TABLE | VIEW | COLUMN} name IS 'text' | NULL`, as in
  Postgres (dbt's `persist_docs` runs it). A column is named `t.col`,
  `schema.t.col` or `redis.schema.t.col`, and names resolve as elsewhere
  (temporary objects first). `NULL` or `''` removes the comment.
  - **Storage:** the comment is part of the table's or view's metadata
    (changed with `WATCH`/`MULTI`), so it stays with the object through
    `RENAME TO` and `RENAME COLUMN`, and is dropped with the object, or with
    its column by `DROP COLUMN`. `TRUNCATE` keeps it. With
    `adbc.redis.rename_rekey` the comments move with the metadata, and
    `COMMENT ON` isn't refused while the rows move. `CREATE OR REPLACE VIEW`
    keeps the view's comment and those of the columns that keep their names,
    as Postgres does. `CREATE TABLE … AS` and `CREATE VIEW` copy no comments
    from what they read.
  - **Reading them back:** `information_schema.tables.comment` and
    `information_schema.columns.comment`; `GetObjects`' `remarks` for
    columns (the `GetObjects` schema has no field for a table's comment);
    and `GetTableSchema`, as the field metadata `ARROW:FLIGHT:SQL:REMARKS`
    (the key Flight SQL uses for column remarks) of each column that has
    one. Postgres's `obj_description` / `col_description` aren't supported:
    they take OIDs, which the driver doesn't have.
  - **Errors** are Postgres's: `"v" is not a table` (`COMMENT ON TABLE` on a
    view), `"t" is not a view`, `relation "t" does not exist`, `column "c"
    of relation "t" does not exist`.

### Inserts, parameters, defaults, and constraints

- `INSERT INTO t [(cols)] VALUES (…), (…)` with literals, `?` / `$n`
  parameters or `DEFAULT`, `INSERT INTO t DEFAULT VALUES`, and
  `INSERT INTO t [(cols)] SELECT …` (the query may be parenthesized:
  `INSERT INTO t (SELECT …)`)
- **Parameters.**
  - **Placeholders:** `$1`, `$2`, … and `?`. A `?` is numbered in order of
    appearance, separately from `$n`: in `SELECT $2, ?` the `?` is the first
    parameter.
  - **Binding:** with `Bind` or `BindStream`; the statement runs once per row.
  - **`GetParameterSchema`** describes the parameters without running the
    statement: one unnamed, nullable field per parameter, in order, with the
    type the parameter is used as:
    - Compared with an expression (`=`, `<>`, `<`, …, `IS [NOT] DISTINCT
      FROM`, and so `IN (…)` and `BETWEEN`): that expression's type. In `$1
      IN (SELECT …)`, the subquery's column type.
    - An `INSERT` value, or an `UPDATE` `SET` value: the target column's type.
      This also covers `-$1` and the select items of `INSERT … SELECT`.
    - `CAST($1 AS t)` or `$1::t`: `t`.
    - Arithmetic (`+`, `-`, `*`, `/`, `%`): the type a quoted literal has
      there (see Quoted string literals), as in Postgres. With a number,
      that number's type; `ts + $1` is an interval, `ts - $1` a timestamp,
      `d - $1` a date and `iv * $1` a double; none for `d + $1`, which could
      be either, or `b + $1`.
    - `||`, `LIKE`, `ILIKE`, `SIMILAR TO` and the regex operators: text.
    - `AND`, `OR`, `NOT`, and `CASE WHEN` conditions: boolean.
    - `COALESCE`, `NULLIF`, `GREATEST` and `LEAST` arguments, and `CASE`
      results: the type of the others.
  - **Which use wins:** the first use that gives a type.
  - **Null type:** a parameter gets the null type when it is used nowhere a
    type can be taken from (`SELECT $1`), or only in a statement that needs
    what an earlier statement of the script makes. `MERGE`'s parameters get it
    too.
  - **Errors:** a statement that can't be planned, such as one on an unknown
    table, is an error.
- Column defaults (`DEFAULT expr`), as in Postgres:
  - A row gets a column's default when it is inserted without a value for
    it: a column left out of the column list of `INSERT … VALUES`,
    `INSERT … SELECT` or MERGE's `INSERT`, `DEFAULT` in place of a value,
    `INSERT … DEFAULT VALUES`, and columns missing from the Arrow data of a
    bulk ingest. An explicit NULL stays NULL (and fails on a `NOT NULL`
    column). `UPDATE … SET col = DEFAULT` is not supported.
  - The default is checked when it is defined: it may not read columns or
    use subqueries, parameters, aggregates, `GROUPING` or window functions,
    its function calls are checked as in a query (`DEFAULT nosuchfunc(NULL)`
    is `function nosuchfunc(unknown) does not exist`), and it must convert
    to the column's type and fit its length (`INTEGER DEFAULT 'abc'` and
    `VARCHAR(3) DEFAULT 'abcd'` are errors then).
    `DEFAULT NULL` is the same as no default.
  - It is computed once per statement (or bulk ingest), so all the rows of
    a statement get the same `CURRENT_TIMESTAMP` / `NOW()` /
    `CURRENT_DATE`, the statement's time. A volatile default (`RANDOM()`)
    is computed for every row.
  - `information_schema.columns.column_default` and `GetObjects`'
    `xdbc_column_def` show the default as written, except that a local-time
    literal is stored as its instant (see Session time zone). `CREATE TABLE
    … AS` copies values, not defaults
- String lengths, as in Postgres:
  - **Types:** `VARCHAR(n)` (also `CHARACTER VARYING(n)`, `CHAR VARYING(n)`
    and `NVARCHAR(n)`) holds at most `n` characters. `CHAR(n)` (also
    `CHARACTER(n)`, `NCHAR(n)` and `BPCHAR(n)`) holds exactly `n`: shorter
    values are padded with spaces. `CHAR` without a length is `CHAR(1)`, and
    `BPCHAR` without one is blank-padded with no limit. `VARCHAR` without a
    length, `VARCHAR(MAX)`, `TEXT`, `STRING` and `CLOB` have no limit.
    Lengths count characters, not bytes (`'日本語'` fits `VARCHAR(3)`), and go
    from 1 to 10485760.
  - **Writes:** a value longer than its column's length is an error, `value
    too long for type character varying(3)` (`character(3)` for `CHAR(3)`),
    with ADBC status `InvalidData` and SQLSTATE `22001` (see
    [Error handling](#error-handling)),
    unless the characters beyond the length are all spaces, which are cut
    (`'ab    '` is written to a `VARCHAR(3)` as `'ab '`). Every write
    checks it: `INSERT … VALUES`, `INSERT … SELECT`,
    `INSERT … DEFAULT VALUES`, `UPDATE` (also with `FROM`), both branches of
    `MERGE`, and bulk ingest. It is checked where `NOT NULL` and `CHECK` are,
    before them, so a statement with a failing row writes nothing (a bulk
    ingest is checked one Arrow batch at a time). A column's `DEFAULT` is
    checked when it is defined.
  - **Casts:** `CAST(x AS VARCHAR(n))` and `x::char(n)` cut the text to `n`
    characters (`CAST('abcdef' AS VARCHAR(3))` is `'abc'`), and `CHAR(n)`
    pads it (`CAST('ab' AS CHAR(3))` is `'ab '`). Only an explicit cast
    cuts; writing a longer value is an error.
  - **`CHAR` values** are stored and returned padded, and their trailing
    spaces don't count: `LENGTH` of `'ab '` in a `CHAR(3)` is 2, it equals
    `'ab'`, and joins, `IN`, `GROUP BY`, `DISTINCT` and set operations treat
    the two as the same value. Converting one to text (functions, `||`, a
    `VARCHAR` column) drops the padding (`c || 'x'` is `'abx'`), but `LIKE`,
    `ILIKE`, `~`, `~*` and `SIMILAR TO` see it (`c LIKE 'ab'` is false, `c
    LIKE 'ab%'` true), as in Postgres. One difference: a comparison with a
    `CHAR` value ignores trailing spaces on both sides, as Postgres does for
    a literal (`c = 'ab '` is true), so a text column holding `'ab '` also
    equals a `CHAR` holding `'ab'`; Postgres compares a `CHAR` with a text
    column as text, which keeps them.
  - **Result types** keep the length where Postgres does: a column, a cast,
    a scalar subquery, and `CASE`, `COALESCE`, `NULLIF`, `GREATEST`, `LEAST`
    or a set operation whose inputs all have the same type. Function,
    aggregate and window results have none, nor does a mix of lengths or a
    string literal (`COALESCE(s, 'x')`). So `CREATE TABLE … AS` and views
    keep the length of a column or cast they select, and a computed column
    is `VARCHAR`.
  - **Reading them back:** `GetObjects` reports the type as `VARCHAR(n)` /
    `CHAR(n)` in `xdbc_type_name`, with `xdbc_column_size` `n` and
    `xdbc_char_octet_length` `4n` (UTF-8); `information_schema.columns` has
    `character_maximum_length` and `character_octet_length` (NULL and
    1073741824 for a string without a length, as in Postgres) and the same
    type in `data_type`.
  - **Upgrading:** v0.0.7 and earlier didn't keep the length, so the tables
    they created have none and aren't checked; their values stay as they
    are. Only tables created and columns added from this version on are
    checked. `CHAR` without a length was unbounded, and is now `CHAR(1)`.
    Views, `CHECK`s and defaults that cast to `VARCHAR(n)` / `CHAR(n)` now
    cut. Clients on v0.0.7 and earlier ignore the length (an optional part
    of the column type in the metadata), so they don't check it, and a
    change they make to a table's metadata (an `ALTER TABLE`, a `COMMENT
    ON`, and some writes) removes it, so upgrade every client of a database
    together.
- Constraints, written as in Postgres. `NOT NULL`, `CHECK` and the lengths
  of `VARCHAR(n)` / `CHAR(n)` (see String lengths above) are enforced.
  `PRIMARY KEY` is kept and makes its columns `NOT NULL`, but its
  uniqueness isn't enforced. `UNIQUE` and `FOREIGN KEY` are not enforced:
  - **Accepted:** on a column (in `CREATE TABLE` and `ADD COLUMN`), any
    number of `NOT NULL`, `NULL`, `CHECK (expr)`, `DEFAULT expr`, `PRIMARY
    KEY`, `UNIQUE` and `REFERENCES t [(col)] [MATCH {FULL | PARTIAL |
    SIMPLE}] [ON DELETE action] [ON UPDATE action]`, in any order, each
    optionally named with `CONSTRAINT name`. As a table constraint (in
    `CREATE TABLE` and `ALTER TABLE … ADD`), `[CONSTRAINT name]` followed by
    `CHECK (expr)`, `PRIMARY KEY (cols)`, `UNIQUE (cols)` or `FOREIGN KEY
    (cols) REFERENCES t [(cols)] …`. A column `CHECK` means the same as a
    table one: a condition on the whole row, which may read any column.
    After `PRIMARY KEY (cols)`, Postgres's index parameters (`INCLUDE
    (cols)`, `WITH (…)`, `USING INDEX TABLESPACE name`) and `DEFERRABLE` /
    `INITIALLY DEFERRED` are accepted and mean nothing.
  - **Not enforced:** `UNIQUE` and `FOREIGN KEY` / `REFERENCES` are
    accepted and ignored. Nothing is kept, so duplicate values and rows
    without a parent are inserted, the referenced table needn't exist, and
    `GetObjects` doesn't list them. Nor is the uniqueness of a `PRIMARY KEY`
    enforced (see "Primary keys" below).
  - **`CHECK`, when written:** the expression is checked as in Postgres. It
    may read the table's columns (`col`, `t.col` or `schema.t.col`, see
    "Names" below) but not `__rowid` or another table, may not use
    subqueries, parameters, aggregates,
    `GROUPING` or window functions, and must be boolean. Its function calls
    are checked as in a query (see "Function calls" below). The errors are
    `column "x" does not exist in table "t"`, `system column "__rowid"
    reference in check constraint is invalid`, `missing FROM-clause entry
    for table "u"`, `cannot use subquery in check constraint`, `cannot use
    parameter in check constraint`, `aggregate functions are not allowed in
    check constraints`, `grouping operations are not allowed in check
    constraints`, `window functions are not allowed in check constraints`,
    `function nosuchfunc(integer) does not exist`, `UPPER expects 1
    argument` and `argument of CHECK must be type boolean, not type
    integer`. A value the expression can't compute for a row (`LOG(0)`) is
    an error when that row is written.
  - **Names:** `CONSTRAINT name` is used as written. Without one, the name
    is Postgres's: `<table>_<column>_check` if the expression reads exactly
    one column (`t_amount_check`, also for a table constraint such as
    `CHECK (id > 0)`), otherwise `<table>_check`, with `1`, `2`, … appended
    to a name the table already uses (`t_amount_check1`). Two constraints
    of a table can't have the same name (`check constraint "c" already
    exists`; `ADD`: `constraint "c" for relation "t" already exists`).
  - **Enforcement:** every write checks each new or changed row: `INSERT …
    VALUES`, `INSERT … SELECT`, `INSERT … DEFAULT VALUES`, `UPDATE` (also
    with `FROM`), both branches of `MERGE`, and bulk ingest. As in SQL, a
    row fails only if the expression is FALSE; NULL passes. The row is
    checked after its defaults are filled in and after `NOT NULL`, with
    the error `new row for relation "t" violates check constraint
    "t_amount_check"`. Like `NOT NULL`, this happens while the statement
    computes its changes, so a statement with a failing row writes
    nothing; a bulk ingest is checked one Arrow batch at a time, so the
    batches before a failing one stay written. As in Postgres, the
    constraints are checked in name order, so a row that fails several
    reports the first, and an `UPDATE` checks every constraint, also those
    that don't read the columns it sets (it reads the columns they use).
  - **Existing rows:** `ADD COLUMN … CHECK` and `ADD [CONSTRAINT name] CHECK`
    check every row first, with the new column at its default, and fail
    with `check constraint "c" of relation "t" is violated by some row`.
    Rows that other connections insert while that runs are checked when the
    change commits, but not all the rows they write (see "Concurrent
    writes" under `ALTER TABLE`).
  - **Lifetime:** the constraints are part of the table's metadata (an
    optional `checks` field holding each name and expression, so older
    metadata reads unchanged). `RENAME TO` (also with
    `adbc.redis.rename_rekey`) and `TRUNCATE` keep them, with their names;
    errors name the table as it is now. `RENAME COLUMN` rewrites the
    expressions that read the column. `DROP COLUMN` drops the constraints
    that read it, also those that read other columns too, and `DROP
    CONSTRAINT [IF EXISTS] name` drops one. Temporary tables have them in
    the same way; `CREATE TABLE … AS` copies none.
  - **Primary keys,** as in Postgres 16:
    - **Definition:** a table has at most one (`multiple primary keys for
      table "t" are not allowed`), of its own columns (in `ALTER TABLE`,
      also those the statement adds), each named once: `column "x" named
      in key does not exist` (`ALTER TABLE`: `column "x" of relation "t"
      does not exist`) and `column "a" appears twice in primary key
      constraint`.
    - **`NOT NULL`:** the key's columns become `NOT NULL` as if declared so
      (also a column declared `NULL`), so every write checks them, with
      Postgres's `null value in column "id" of relation "t" violates
      not-null constraint`. `information_schema.columns.is_nullable`,
      `GetObjects` and `GetTableSchema` show it. `DROP NOT NULL` on a key
      column fails (`column "id" is in a primary key`), and the columns stay
      `NOT NULL` when the key is dropped.
    - **Names:** `CONSTRAINT name` as written, otherwise `<table>_pkey`,
      with `1`, `2`, … appended to a name one of the table's `CHECK`s uses
      (`t_pkey1`). A `CHECK` added later avoids the key's name in the same
      way. In `CREATE TABLE` the `CHECK`s are named first, so a key named
      like one of them fails with `constraint "c" for relation "t" already
      exists`, as in Postgres. Postgres names the key's index the same, so
      it also avoids the names of the schema's other tables, indexes and
      constraints: where another table's key is `t_pkey` already (it keeps
      that name when its table is renamed), Postgres names a new table
      `t`'s key `t_pkey1` and the driver `t_pkey`.
    - **Uniqueness isn't enforced:** rows with the same key are inserted,
      and `UPDATE` can give two rows the same key. Enforcing it needs a
      check that is atomic with the write against every other writer, and
      the rows are separate HASHes (on different shards of a cluster),
      written without a transaction across them: looking the key up in the
      search index first would let two statements each find no row and
      both insert one. So `information_schema.table_constraints.enforced`
      is `NO` for a primary key, and the dbt adapter declares `primary_key`
      `not_enforced` (dbt warns about it in a contract, as for Snowflake or
      BigQuery).
    - **Existing rows:** `ADD PRIMARY KEY` and `ADD COLUMN … PRIMARY KEY`
      check every row for NULLs in the key's columns as `SET NOT NULL` does,
      with the same error (`column "id" of relation "t" contains null
      values`) and the same treatment of concurrent writes (see "Concurrent
      writes" under `ALTER TABLE`). Duplicate keys aren't checked
      (Postgres's `could not create unique index "t_pkey"`).
    - **Lifetime:** the key is part of the table's metadata (an optional
      `primary_key` field: its name and the HASH fields of its columns, in
      key order). `RENAME COLUMN` keeps it on the renamed column. `RENAME
      TO` (also with `adbc.redis.rename_rekey`) and `TRUNCATE` keep it with
      its name, as Postgres keeps its index's. `DROP COLUMN` of one of its
      columns drops it, as `DROP CONSTRAINT name` does. Temporary tables
      have one in the same way; `CREATE TABLE … AS` and a bulk ingest that
      creates its table don't.
  - **Reading them back:** `GetObjects`' `table_constraints` lists the
    primary key, with type `PRIMARY KEY` and its columns in key order, and
    each `CHECK`, with type `CHECK` and the columns its expression reads.
    `information_schema.table_constraints` lists both, with Postgres's
    columns (`is_deferrable` and `initially_deferred` are `NO`; `enforced`
    is `YES` for a `CHECK` and `NO` for a primary key), and
    `information_schema.key_column_usage` the key's columns, with their
    `ordinal_position` in the key. Postgres 16 also lists each `NOT NULL`
    in `table_constraints`, as a `CHECK` named with object ids
    (`2200_16384_1_not_null`); the driver doesn't (see
    `information_schema.columns.is_nullable`).
  - **Upgrading:** clients on v0.0.6 and earlier ignore the new metadata.
    They don't check the constraints, and a change they make to a table's
    metadata (an `ALTER TABLE`, a `COMMENT ON`, and some writes) removes
    them, so upgrade every client of a database together. Earlier versions
    accepted table-level `CHECK`s without storing them, so tables they
    created don't enforce theirs; add them again with `ALTER TABLE … ADD
    CHECK (…)`. Earlier versions also accepted a constraint that calls an
    unknown function; every write to its table now fails with `check
    constraint "c" of relation "t" can't be evaluated (function
    nosuchfunc(integer) does not exist); drop it with ALTER TABLE … DROP
    CONSTRAINT`.
  - **Upgrading from v0.0.12 and earlier:** `PRIMARY KEY` was accepted and
    not kept, so the tables those versions created have no key, and its
    columns are nullable unless they were declared `NOT NULL`; add it with
    `ALTER TABLE … ADD PRIMARY KEY (…)`. A table can no longer have two
    `PRIMARY KEY` clauses. Clients on those versions ignore the
    `primary_key` field (they still check the `NOT NULL` of its columns,
    which is part of each column), and a change they make to a table's
    metadata removes the key. A NULL in a `NOT NULL` column was `NULL value
    in column "a" violates not-null constraint`; it is now Postgres's `null
    value in column "a" of relation "t" violates not-null constraint`.

### Queries and relational features

- `[WITH [RECURSIVE] name [(cols)] AS (SELECT …), …] SELECT [ALL | DISTINCT | DISTINCT ON (…)] … FROM item {, item |
  [INNER | LEFT | RIGHT | FULL] [OUTER] JOIN item ON … | USING (…) | CROSS JOIN item |
  NATURAL [INNER | LEFT | RIGHT | FULL] [OUTER] JOIN item}
  [WHERE …] [GROUP BY …] [HAVING …] [WINDOW w AS (…), …] [QUALIFY …]
  [ORDER BY …] [LIMIT n] [OFFSET m [ROW | ROWS]] [FETCH {FIRST | NEXT} [n] {ROW | ROWS} ONLY]`,
  where an item is a table, a CTE, `[LATERAL] (SELECT …)` or
  `[LATERAL] GENERATE_SERIES(…)`, each with an optional alias and column
  aliases (`AS a(x, y)`; `t.col` qualifies a column, and `t.*` selects one
  item's columns, also as `schema.table.col` and `schema.table.*`; see
  "Names" below),
  with aggregates (below), `CASE` (simple and searched), arithmetic,
  `CAST(x AS type)` / `x::type` (and `TRY_CAST`, see below), `IS [NOT] NULL`,
  `IS [NOT] DISTINCT FROM` (NULLs count as equal), row constructors
  (`(a, b) = (1, 'x')`, `(a, b) IN (SELECT …)`, `SELECT (a, b)`; see below), `[NOT] LIKE` / `ILIKE` (with `ESCAPE`), subqueries (scalar `(SELECT …)`, `EXISTS`,
  `[NOT] IN (SELECT …)`, `x op ANY | SOME | ALL (SELECT …)`, also with a row
  on the left, correlated or not, in SELECT/WHERE/HAVING and in
  `UPDATE`/`DELETE`/`MERGE`),
  `BETWEEN`, `IN`, `COALESCE`, `LOWER/UPPER/LENGTH/ABS`, `CONCAT(a, …)` and
  `CONCAT_WS(sep, a, …)` (NULL arguments are skipped, as in Postgres; `||`
  returns NULL if either side is NULL), `from_hex`
- **Names** resolve as in Postgres, at every query level: subqueries
  (`EXISTS`, `IN`, `ANY` / `ALL`, scalar), `LATERAL`, derived tables, CTEs,
  views, set operations, and the subqueries of `UPDATE`, `DELETE`, `UPDATE …
  FROM`, `DELETE … USING` and `MERGE`:
  - **A FROM item with an alias is visible only by its alias.** In `select
    id from t where exists (select 1 from t x where x.v > t.v)`, `t.v` is
    the outer row's, since the inner `t` is visible only as `x`. Up to
    v0.0.7, a level with a single FROM item also knew it by its table's
    name, so `t.v` was the inner row's and the query compared each row with
    itself (#102)
  - **A FROM item without an alias** is visible by its name. A table or
    view without an alias is also visible as `schema.table.col` (or
    `redis.schema.table.col`), with its own schema only (`pg_temp` for a
    temporary table): in `select s2.t.v from s1.t`, `s2.t` names no FROM
    item. Up to v0.0.8 the schema wasn't checked, so that query read
    `s1.t`'s `v` (#111). CTEs, derived tables and items with an alias
    aren't visible schema-qualified
  - **Two tables or views with the same name** from different schemas can
    be FROM items together without aliases, as in Postgres: `select s1.t.v,
    s2.t.v from s1.t join s2.t on s1.t.id = s2.t.id`, in any join, `USING`
    and `NATURAL` included, and in `UPDATE … FROM` and `DELETE … USING`. Up
    to v0.0.8 that was `table name "t" specified more than once; use
    aliases`. Any other two items of a level with one name still are: the
    same table twice, or an item and an alias, CTE or derived table of its
    name. A `MERGE` source can't have the target's name even then: `name
    "t" specified more than once; the name is used both as MERGE target
    table and data source`
  - **A derived table or CTE reads the queries around it**, as in
    Postgres: a derived table the columns of the queries around the one
    whose FROM list holds it (not that query's other FROM items, which
    need `LATERAL`), and a CTE those around the query whose WITH list
    holds it, wherever it is read (not that query's FROM items, nor the
    queries between). `select * from t where exists (select * from u,
    (select t.v) s)` reads `t.v` of each row of `t`, and so does `select
    (with c as (select t.v) select (select v from c)) from t`; that
    holds for recursive CTEs too. Up to v0.0.12 they couldn't read an
    enclosing query (`missing FROM-clause entry for table "t"`), and a
    CTE's body saw the WITH lists of the queries that read it: in `with c
    as (select * from d) select * from (with d as (select 1) select *
    from c) s`, `d` was the inner CTE (Postgres: no such table)
  - **`t.col`** is a column of the innermost level with an item visible as
    `t`. If that item has no column `col`, it is an error, even when an
    outer `t` has one; if two items of that level are visible as `t`
    (`s1.t` and `s2.t`), it is `table reference "t" is ambiguous`. An
    unqualified `col` is a column of the innermost level that has one, and
    `column reference "col" is ambiguous` if two of that level's items
    have it
  - **Errors** are Postgres's:
    - `missing FROM-clause entry for table "t"` when no level has an item
      visible as `t`, or that reads the table `t`
    - `invalid reference to FROM-clause entry for table "t"; perhaps you
      meant to reference the table alias "x"` (Postgres's hint) when a
      level reads `t` as `x`, as in `select t.id from t x`, `select
      s.t.id from s.t x` or `update t x set … where t.id = 1`. For an
      unqualified `t`, Postgres gives it only when `t` is on the search
      path, the driver whenever an item's table, view or CTE is named `t`
    - `invalid reference to FROM-clause entry for table "t"; there is an
      entry for table "t", but it cannot be referenced from this part of
      the query` (Postgres's detail) when a level has an item named `t`
      that the reference can't name: `select s2.t.v from s1.t`, `select
      s.t.v from s.t t`, or a CTE `t` read as `s.t.v`
    - `cross-database references are not implemented: other.s.t.col` for
      a catalog other than `redis`, and `improper qualified name (too many
      dotted names): a.b.c.d.e` for more than four names (also for `*`)
    - The same error with Postgres's hint, `…; to reference that table,
      you must mark this subquery with LATERAL`, when a derived table
      refers to an earlier FROM item without `LATERAL`: `select * from t,
      (select t.v) s`, also from deeper in the derived table. The detail
      names the item, `x` for `t` read as `x`. An unqualified column of
      such an item is `column "v" does not exist; there is a column named
      "v" in table "t", but it cannot be referenced from this part of the
      query; to reference that column, you must mark this subquery with
      LATERAL`, or, if several items have it, `…; there are columns named
      "v", but they are in tables that cannot be referenced from this part
      of the query; try using a table-qualified name`. As in Postgres,
      there is no hint where `LATERAL` can't help: in the arguments of a
      FROM function (which are `LATERAL` already), and on the right of a
      `RIGHT` or `FULL` join. Up to v0.0.12 these were `missing
      FROM-clause entry for table "t"` and `column "v" does not exist`
    - The same error without a hint when a FROM item of `UPDATE t …
      FROM`, `DELETE FROM t USING` or a `MERGE INTO t` source reads the
      target, which `LATERAL` can't make visible: `update t set v = 0 from
      u, (select t.v) s`. An unqualified column of the target is `column
      "v" does not exist; there is a column named "v" in table "t", but it
      cannot be referenced from this part of the query`. As in Postgres, a
      `LATERAL` item or a FROM function of `UPDATE` or `DELETE` that reads
      the target is `invalid reference to FROM-clause entry for table "t";
      there is an entry …` even for an unqualified column that an earlier
      item has too. Up to v0.0.12 these were `missing FROM-clause entry
      for table "t"`, and `update t set v = 0 from u, lateral (select v)
      s` read `u.v`
    - The same errors for a join's `ON` clause among the FROM items of
      `UPDATE … FROM` and `DELETE … USING`, which, as in Postgres, reads
      only that join's two sides, not the target: `update t set v = 0 from
      u join w on w.id = t.id`, also from a subquery in it. `WHERE` reads
      the target, and so does `MERGE`'s `ON`, which joins it. Up to
      v0.0.12 such an `ON` read the target
    - The same errors for a join's `ON` clause that reads a FROM item
      outside its join, in any query: as in Postgres, it reads only the
      join's two sides, the items from the last comma up to it (see
      "Commas" below). `select * from a, b join c on c.x = a.x` is
      `invalid reference to FROM-clause entry for table "a"; there is an
      entry …` (`from a cross join b join c on c.x = a.x` works), an
      unqualified column that only `a` has is `column "y" does not exist;
      there is a column named "y" in table "a", …`, and `select * from a
      join b on b.x = c.x join c on true` is `missing FROM-clause entry for
      table "c"`, also from a subquery in the `ON`. Up to v0.0.12 an `ON`
      read every FROM item of its query
- **Commas** separate FROM-list items, as in Postgres: `a, b join c on …`
  is `a` and `b join c on …`, while `a cross join b join c on …` is
  `(a cross join b) join c on …`. So a join after a comma has only the
  items since the comma on its left: its `ON` can't read `a` (above),
  `USING` and `NATURAL JOIN` match the columns of `b`, not `a`, and a
  `RIGHT` or `FULL` join keeps every row of `a`: `select * from a, b right
  join d on d.x = b.x` is `a` cross joined with `b right join d`, so a row
  of `d` that matches no `b` comes with each row of `a`. `SELECT *` lists
  each FROM-list item's columns in turn. Up to v0.0.12 a comma was `CROSS
  JOIN`: that query gave NULL for `a` in such a row, and `d`'s rows when
  `a` had none, and `from a, e natural join c` joined `a` with `c` (or
  failed with `common column name "x" appears more than once in left
  table`). The `USING` errors are now Postgres's too: `column "x"
  specified in USING clause does not exist in left table` (and `… right
  table`), `common column name "x" appears more than once in left table`
  and `column name "x" appears more than once in USING clause` (up to
  v0.0.12: `USING column "x" is ambiguous`, `USING column "x" does not
  exist in the left side of the join`, and `USING (x, x)` was accepted)
- **Column names** are Postgres's. A select-list item is named by its
  alias, or else by its expression (Postgres's FigureColname): a column by
  the column, `CAST(x AS t)` and `x::t` by `x` (by the type if `x` has no
  name: `int4`, `varchar`, `date`), a call by the function (`count`,
  `upper`; also `extract`, `btrim`, `timezone` for `EXTRACT`, `TRIM` and
  `AT TIME ZONE`), a scalar subquery by its column, `EXISTS` by `exists`,
  `CASE` by its `ELSE` (or `case`), a row by `row`, a field `(r).f1` by
  the field, and an operator or a literal is `?column?`. `SELECT count(*),
  upper(v), CAST(iv AS VARCHAR), v || 'x', 1 FROM t` has the columns
  `count`, `upper`, `iv`, `?column?` and `?column?`. These name the
  result's columns (the Arrow fields, also from `ExecuteSchema`),
  `RETURNING`'s, and those of derived tables, CTEs, views and `CREATE
  TABLE … AS`, so `SELECT count FROM (SELECT count(*) FROM t) s` and
  `SELECT * FROM (SELECT CAST(iv AS VARCHAR) FROM t) s ORDER BY iv` work
  as in Postgres
  - **Names that repeat** (`SELECT 1, 2`, `SELECT count(*), count(v)`,
    `SELECT a, a`, `AS s(x, x)`): a result, a derived table or a CTE keeps
    them all, as in Postgres, and only reading such a column by its name is
    an error: `column reference "count" is ambiguous` (also as `s.count`),
    `ORDER BY "count" is ambiguous`, and in `USING` or `NATURAL JOIN`
    `common column name "count" appears more than once in left table` (or
    `right table`). A position (`ORDER BY 2`), `*` or column aliases (`AS
    s(a, b)`) read them. A table or view can't have them: `CREATE TABLE …
    AS` and `CREATE VIEW` are `column "count" specified more than once`,
    as in Postgres; give the columns aliases. Column aliases on a table
    (`FROM t AS x(a, a)`) still can't repeat a name, which Postgres allows
    until one is read
  - Names are compared without case, as elsewhere in the driver: in
    `SELECT a FROM (SELECT 1 AS "A", 2 AS a) s`, `a` is ambiguous, where
    Postgres reads the second column
  - **Upgrading from v0.0.12 and earlier:** an item without an alias that
    wasn't a column was named by its text, `count(*)`, `CAST(iv AS
    VARCHAR)`, `v || 'x'`, `1`, so result field names change: code that
    reads columns by name, unaliased expressions included, needs the new
    names, or aliases. Tables that `CREATE TABLE … AS` made and views keep
    the names they were made with; made again, they get the new names, and
    a query whose items now share a name (`SELECT count(*), count(v)`,
    `SELECT 1, 2`) needs aliases. A view whose query reads a derived
    table's or CTE's column by its old name (`SELECT "count(*)" FROM
    (SELECT count(*) FROM t) s`) is now `view "v" is no longer valid:
    column "count(*)" does not exist`; replace it with `CREATE OR REPLACE
    VIEW`. Derived tables and CTEs whose columns shared a name were
    `column name "a" appears more than once in "s"; add aliases`, a view's
    `column name "a" appears more than once in view "v"; add aliases`, and
    a table's `duplicate column name "a"`. The `ambiguous` errors had no
    SQLSTATE; they are `42702` now
- `FETCH FIRST n ROWS ONLY` is `LIMIT n` (`FETCH FIRST ROW ONLY` is `LIMIT
  1`); `WITH TIES` is not supported, and a query has at most one `LIMIT` or
  `FETCH` (Postgres's "multiple LIMIT clauses not allowed")
- A query that can't return rows (`WHERE false`, `LIMIT 0`, …; see "How SQL
  is executed") reads none. It is still planned, so unknown tables, columns
  and functions and wrong argument counts are errors. Errors the driver
  raises only when it computes a value on a row (division by zero, a failed
  cast, the logarithm of zero) are then not raised, as on an empty table:
  `SELECT 1/0 FROM t WHERE false` returns no rows
- `GROUP BY` items are expressions, output positions or aliases, and also
  (as in Postgres) `ROLLUP (…)`, `CUBE (…)`, `GROUPING SETS (…)` and `()`:
  - A bare name is an input column's before it is an output column's
    (unlike in `ORDER BY`), as in Postgres. A name no input column has is
    the output column of that name, also one without an alias, named by
    its expression (see "Column names" above): after `SELECT upper(v)`,
    `GROUP BY upper` groups by `upper(v)`; after `SELECT CAST(x AS
    INTEGER)`, `GROUP BY x` is still the column `x`. Only the query's own FROM items count, not
    those of the queries around it; a name two of them have (`k` of `JOIN …
    USING (k)`, where the driver keeps both columns) may also be an output
    column's. Output columns of the same name must be the same expression,
    otherwise it is `GROUP BY "a" is ambiguous`, and the one named may not
    be an aggregate or a window function. Unlike in Postgres, an alias may
    also be used inside an expression (`GROUP BY lower(u)` after `upper(v)
    AS u`); a name without an alias may not
  - **Upgrading from v0.0.12 and earlier:** a bare name matched only
    aliases, so `GROUP BY upper` after `SELECT upper(v)` was `column
    "upper" does not exist`, or in a subquery read an enclosing query's
    column `upper`. Of two aliases of the name the last one was used, where
    it is now ambiguous. In a join an alias came before an input column of
    its name, and `GROUP BY k` after `SELECT t.k … JOIN … USING (k)` was
    `column reference "k" is ambiguous`
  - `ROLLUP (a, b)` is the grouping sets `(a, b), (a), ()`, and `CUBE (a, b)`
    is every subset of its items (at most 12). `GROUPING SETS (…)` lists
    sets, and may contain `ROLLUP`, `CUBE` and `GROUPING SETS`. A
    parenthesized list is one item of several expressions (`ROLLUP (a, (b,
    c))`), and `()` is the empty set: one group of all the rows, even when
    there are none
  - Several items combine as a cross product: `GROUP BY a, ROLLUP (b, c)` is
    `(a, b, c), (a, b), (a)`. At most 4,096 sets; repeated sets are kept
  - A set's rows have NULL for the grouping columns it doesn't group.
    `GROUPING(a, …)` tells those from NULL values: an `INTEGER` with a bit
    per argument, the last one lowest, set when the argument is not grouped.
    It works in the SELECT list, HAVING, ORDER BY, QUALIFY and window
    definitions (and is 0 in a plain `GROUP BY`)
  - As in Postgres, outside aggregate calls such a query may use only
    grouping expressions, matched as a whole (`GROUP BY ROLLUP (UPPER(s))`
    can select `UPPER(s)` but not `s`); this also applies to a plain `GROUP
    BY` that calls `GROUPING()`
  - Values that compare equal are one group, as in Postgres: `INTERVAL '1
    mon'` and `INTERVAL '30 days'`, `INTERVAL '1 day'` and `INTERVAL '24
    hours'`, and a `DOUBLE PRECISION` `-0` (`-x` of a zero) and `0`. The
    group shows the value of its first row read, which on a cluster can be
    either. So do `SELECT DISTINCT` (which runs as `GROUP BY`), grouping
    sets and `DISTINCT` in aggregates, as window partitions, `DISTINCT ON`,
    set operations and joins already did
  - A `CASE` or `COALESCE` of a number and a string literal gives numbers,
    as in Postgres (`COALESCE(n, '0')` of an integer `n` is an integer, and
    its `'0'` the integer 0; see "Quoted string literals"), so a `0` of `n`
    and the `'0'` are one group, also for `DISTINCT` and window partitions
  - **Upgrading from v0.0.12 and earlier:** `GROUP BY`, `SELECT DISTINCT`
    over plain expressions and `DISTINCT` in aggregates told such values
    apart by their text, so `1 mon` and `30 days` were two groups, and
    `SUM(DISTINCT iv)` added both. `DISTINCT` in aggregates and window
    partitions also told the `0` from the `'0'`. And two rows of several
    values could be taken for one when they held NUL bytes (`BYTEA` values,
    or text that was bound or ingested), in `GROUP BY`, `SELECT DISTINCT`,
    set operations and window partitions
  - Unlike in Postgres, `GROUP BY`, `HAVING` and `QUALIFY` expressions may
    use the select list's aliases (`HAVING n > 1` after `count(*) AS n`). A
    bare name is an alias's only if no FROM item of the query has a column
    of that name, so an input column comes first, also in a join. An
    enclosing query's column comes after the alias, and a name two FROM
    items have (`k` of `JOIN … USING (k)`) may be an alias's too. A
    qualified name (`t.x`) is never an alias's
  - **Upgrading from v0.0.12 and earlier:** in a join an alias came before
    an input column of its name in these expressions too, even a qualified
    one: after `SELECT CAST(x AS INTEGER) AS x … FROM t JOIN u …`,
    `HAVING x > 1.3`, `HAVING t.x > 1.3` and `GROUP BY t.x` read the alias.
    And a query planned again (a statement executed again or with
    parameters, or a subquery in a `GROUP BY` item named by position or
    alias, as in `GROUP BY 1, 2`) resolved its `GROUP BY` again from the
    first plan's result: after `SELECT id AS w, max(v) AS id … GROUP BY w`
    in a subquery, where `id` is an enclosing query's column, `w` had
    become `id`, which then named the alias `id`: `aggregate functions are
    not allowed in GROUP BY`
- **Subqueries of a grouped query** (`GROUP BY`, or aggregates or `HAVING`
  without it) may read only its grouped columns, as in Postgres. That applies
  where the query computes a value per group: the SELECT list, `HAVING`,
  `ORDER BY`, `QUALIFY`, `DISTINCT ON` and window definitions, outside
  aggregate calls, also in nested subqueries. A grouped column is a `GROUP
  BY` item by itself, so `group by v` lets a subquery read `g.v`, but
  `group by id + 1` doesn't let it read `g.id`.
  - **The error** is Postgres's `subquery uses ungrouped column "g.id" from
    outer query`, for example in `select v from g group by v having exists
    (select 1 from g x where x.id > g.id)`. Up to v0.0.8 the subquery read
    one row of each group (#112).
  - **Aggregates of the outer query:** as in Postgres, an aggregate belongs
    to the innermost query whose columns its arguments, `ORDER BY` and
    `FILTER` read. `max(g.id)` in `(select max(g.id) from x where x.id = 1)`
    is the outer query's: it is computed over each of its groups, and is a
    constant in the subquery (also in its `WHERE`). It makes a query without
    `GROUP BY` an aggregate one, which returns one row. `sum(g.v + x.id)`
    reads the subquery's columns too, so it is the subquery's. Up to v0.0.9
    such aggregates were computed over the subquery's rows, or rejected as
    not supported (#120).
  - **Where an aggregate may be** is decided at its own level, also for one
    in a subquery: `select id from g where (select max(g.id)) > 1` gives
    Postgres's `aggregate functions are not allowed in WHERE`. These errors
    (in `WHERE`, `GROUP BY`, `JOIN` conditions, `UPDATE`, `VALUES`,
    `RETURNING`, `FILTER`, MERGE `WHEN` conditions, …) have Postgres's text;
    up to v0.0.9 some said `aggregates are not allowed in …`.
  - **Not supported:** such an aggregate with a subquery in its arguments,
    as in `(select max((select g.id)) from x)`. It is `outer-level aggregate
    with a subquery in its arguments is not supported`.
  - **Repeated subqueries:** a subquery in `GROUP BY` counts as grouped
    where it is the same item, by position or alias (`group by 1`), not
    where it is written again (Postgres accepts both).
  - **Outside subqueries,** a column that is neither grouped nor aggregated
    is still read from the group's first row. Postgres rejects it unless the
    `GROUP BY` has the table's primary key.
- `UNION [ALL]`, `INTERSECT [ALL]`, `EXCEPT [ALL]` (`INTERSECT` binds
  tighter; parenthesized branches may have their own `ORDER BY`/`LIMIT`).
  Columns are matched by position and widened to a common type (a column of
  quoted literals takes the other branch's type, see "Quoted string
  literals" below); NULLs count as equal when removing duplicates. `ORDER BY`
  on the combined result uses output column names (the first branch's, named
  as in Postgres; see `ORDER BY` below) or positions. Set operations work
  anywhere a query does (subqueries, CTEs, views, CTAS, `INSERT … SELECT`)
- `WITH RECURSIVE name [(cols)] AS (non-recursive term UNION [ALL]
  recursive term)`, evaluated as in Postgres: the recursive term reads the
  rows the previous iteration added until it adds none, and `UNION` also
  drops rows equal to any earlier row (NULLs count as equal). The columns
  are named by the CTE's column list or the non-recursive term, and have the
  non-recursive term's types, widened to hold the recursive term's values of
  the same category (otherwise it's Postgres's "has type … in non-recursive
  term but type … overall" error; cast the non-recursive term). Postgres's
  rules apply, with its errors: the recursive term refers to the CTE once,
  not in a subquery, on the NULL-supplying side of an outer join, under
  `EXCEPT` or `INTERSECT ALL`, or at a level with aggregates; no `ORDER BY`
  / `LIMIT` / `OFFSET` on the `UNION`; no mutual recursion between CTEs. The
  CTE is computed in full before the query reads it (Postgres stops early,
  for instance under a `LIMIT`), so the recursive term needs a stop
  condition: more than 10,000 iterations that add rows, or more than
  1,000,000 rows, is an error. A CTE of a `WITH RECURSIVE` list that doesn't
  refer to itself is an ordinary CTE
  - `SEARCH {BREADTH | DEPTH} FIRST BY col, … SET seq` adds a `BIGINT` column
    that orders the rows breadth or depth first (Postgres has a record or an
    array that sorts the same way); `CYCLE col, … SET mark [TO v DEFAULT d]
    USING path` marks (and doesn't recurse into) a row whose columns repeat a
    row on its path, and adds the path as text in Postgres's format
    (`{(1),(2)}`). As in Postgres, the recursive term must then be a SELECT
    that reads the CTE at its top level; here it also can't use `GROUP BY`,
    `HAVING`, window functions, `LIMIT` or `OFFSET`
- `LATERAL (SELECT …)` in FROM (`, LATERAL`, `CROSS JOIN LATERAL`,
  `[LEFT] JOIN LATERAL … ON …`) reads the columns of the items before it; a
  table function reads them with or without `LATERAL`, as in Postgres. It
  runs once per distinct value of the columns it reads. `RIGHT` and `FULL`
  joins can't have such a reference to their left side (Postgres's error),
  but are fine for a `LATERAL` item that reads none. Postgres also runs a
  `LATERAL` item that reads the FROM-list items before the comma of a
  `RIGHT` or `FULL` join's FROM-list item (see "Commas"), as in `from a,
  b right join lateral (select a.x) s on true`; the driver doesn't (`a
  LATERAL reference to "a" from a later FROM-list item with a RIGHT or FULL
  JOIN is not implemented`)
- `NATURAL [INNER | LEFT | RIGHT | FULL] JOIN` joins on the columns both
  sides have (a cross join if none). As in Postgres, `SELECT *` lists each
  common column once, first, then the left side's other columns and the
  right side's; the merged column is the left one for inner and left joins,
  the right one for right joins and `COALESCE` of both for full joins, and
  an unqualified reference means the merged column (`a.id` still reads
  `a`'s). `USING (…)` keeps its existing behavior (both columns, so an
  unqualified reference is ambiguous)
- `GENERATE_SERIES(start, stop [, step])` in FROM, with Postgres's overloads:
  integers (step 1 by default) and numerics, and timestamps with an interval
  step (`DATE` arguments give `TIMESTAMP WITH TIME ZONE`, as in Postgres;
  cast with `::date`; a `TIMESTAMP WITH TIME ZONE` series adds its step's
  months and days in the session time zone). The column is named
  `generate_series`, or after the alias (`AS g` → `g`, `AS g(n)` → `n`). A zero step is an error, a step in
  the wrong direction or a NULL argument gives no rows, and month steps
  clamp (`Jan 31, Feb 29, Mar 29, …`). Parameters work, and so do joins
  (`generate_series(…) d LEFT JOIN t ON t.day = d::date` for a date spine)
  and arguments read from earlier FROM items. At most 1,000,000 rows per
  call
- `x op ANY | SOME | ALL (SELECT …)` for `= <> < <= > >=`, with SQL's
  three-valued logic: `ANY` is true if a comparison is, else NULL if one is
  NULL; `ALL` is false if a comparison is, else NULL if one is NULL. Over no
  rows `ANY` is false and `ALL` true. `= ANY` is `IN` and `<> ALL` is
  `NOT IN`
- Row constructors, as in Postgres: `(a, b, …)` with two or more items, and
  `ROW(…)` with any number (`ROW(a)`, `ROW()`). Two rows are compared item
  by item wherever a condition can be: `WHERE`, `ON`, `HAVING`, `CASE`, the
  SELECT list, `CHECK`, and the conditions of `UPDATE`, `DELETE` and
  `MERGE`. Anywhere else a row is a value (see Rows as values below):
  - **`=` and `<>`:** item by item. `=` is true if every pair is equal,
    false if a pair is unequal, and otherwise NULL: `(1, NULL) = (1, 2)` is
    NULL, `(1, NULL) = (2, 2)` false. `<>` is its negation
  - **`<`, `<=`, `>`, `>=`:** left to right; the first pair that is unequal
    or has a NULL decides, NULL for a NULL. `(1, 2) < (1, 3)` and
    `(2, NULL) > (1, 5)` are true, `(1, NULL) < (1, 5)` is NULL. Keyset
    pagination writes `WHERE (ts, id) > (?, ?)`; the index finds the rows
    with `ts >= ?` when `ts` is an indexed numeric, date or time column
  - **`IS [NOT] DISTINCT FROM`:** item by item, with NULLs equal
  - **`IS [NOT] NULL`:** `(a, b) IS NULL` is true when every item is NULL,
    `IS NOT NULL` when none is (so a row can be neither)
  - **`[NOT] IN`:** over a list of rows, or a subquery with as many columns
    as the row has items, with three-valued logic: if no row is equal but a
    comparison is NULL (`(1, NULL)` with `(1, 2)`), `IN` is NULL rather than
    false and `NOT IN` NULL rather than true. So dbt's
    `delete+insert` strategy with a list `unique_key`, `delete from t where
    (k1, k2) in (select distinct k1, k2 from …)`, keeps the rows of `t`
    with a NULL key column, as on Postgres
  - **`ANY` / `SOME` / `ALL`** over a subquery: `= ANY` and `= SOME` are
    `IN`, `<> ALL` is `NOT IN`, and the other operators compare with each
    row
  - **`(a, b) op (SELECT x, y …)`** compares with the subquery's row: NULL
    if it returns none, an error if it returns more than one. As in
    Postgres, the subquery comes after the row: `(SELECT x, y …) = (a, b)`
    is `subquery must return only one column`, and `(SELECT ROW(x, y) …) =
    (a, b)` compares two records. Up to v0.0.12 a subquery of several
    columns before the row was compared as one after it
  - **`BETWEEN` and simple `CASE`** work with rows too (`(a, b) BETWEEN (1,
    2) AND (3, 4)`, `CASE (a, b) WHEN (1, 2) THEN …`). `EXISTS` is
    unchanged
  - **Errors**, raised when the statement is planned, are Postgres's:
    `unequal number of entries in row expressions`, `subquery has too many
    columns` / `subquery has too few columns` (also `x IN (SELECT a, b …)`
    for a single value), `cannot compare rows of zero length`, and
    `operator does not exist: record = integer` for a row compared with a
    single value
  - A comparison of two rows is rewritten into comparisons of their items,
    so `(k1, k2) = (1, 'x')` runs in the index like `k1 = 1 AND k2 = 'x'`.
    For `IN`, see [Query execution](#query-execution).
- Rows as values: a row that isn't compared with another row is a value of
  type record, Postgres's anonymous record: `SELECT (a, b)`, `ORDER BY (a,
  b)`, `GROUP BY ROW(a, b)`, `DISTINCT`, a row nested in a compared row, a
  derived table's or CTE's column, a `CASE` or `COALESCE` result. Up to
  v0.0.12 these were errors (`a row constructor can only be compared …`,
  `nested row constructors are not supported`):
  - **Results:** a record is an Arrow struct whose children are named `f1`,
    `f2`, … (Postgres's names for an anonymous record's fields), each
    nullable, with the items' types; a nested row is a nested struct. A
    NULL record is a NULL struct, and a record whose fields are NULL is
    not. The column of `SELECT (a, b)` is named `row`, as in Postgres.
    Postgres's own ADBC driver can't read anonymous records (`Expected
    nested record type to have 1 fields but got 2`), so the struct is the
    driver's choice
  - **Text:** `CAST(r AS VARCHAR)`, `||` and `CONCAT` write a record as
    Postgres does: `(1,"a b",,t)`. A NULL field is empty, and a field is
    quoted when it is empty or has a quote, a backslash, a parenthesis, a
    comma or white space, with quotes and backslashes doubled. Booleans are
    `t` / `f`, binary values `\x…`, and a nested row is quoted text:
    `("(1,2)",3)`. Text can't be read as a record (`input of anonymous
    composite types is not implemented`)
  - **Fields:** `(r).f1`, `(r).f2`, …; `(ROW(1, 2)).f3` is `could not
    identify column "f3" in record data type`. `(r).*` isn't supported
  - **Comparison:** two records (rows nested in compared rows, record
    columns) compare as Postgres's btree comparison of records does: field
    by field, a NULL field equal to a NULL field and greater than any value.
    So `((1, NULL), 3) = ((1, NULL), 3)` is true, while `(1, NULL) = (1,
    NULL)` is NULL. Records of different lengths whose common fields are
    equal can't be compared (`cannot compare record types with different
    numbers of columns`)
  - **`IS [NOT] NULL`:** true for a NULL record or one whose fields are all
    NULL; `IS NOT NULL` for a record none of whose fields is NULL. A field
    that is a record is not NULL, whatever its fields: `((NULL, NULL),
    NULL) IS NULL` is false
  - **Sorting and grouping:** `ORDER BY (a, b)` sorts by the record, as
    Postgres sorts records: NULL fields last in ascending order and first
    with `DESC`. A NULL record sorts the same way unless `NULLS FIRST` /
    `NULLS LAST` places it (other `ORDER BY` keys put NULLs last either
    way). `DISTINCT`, `GROUP BY ROW(a, b)`, `UNION`, `PARTITION BY` and
    `COUNT(DISTINCT …)` treat NULL fields as equal. `GROUP BY (a, b)` is
    `GROUP BY a, b`, as in Postgres. The driver sorts and groups records
    itself, not the index
  - **Functions:** `COALESCE`, `NULLIF`, `GREATEST`, `LEAST`, `IIF`,
    `CASE`, `CONCAT`, `COUNT`, `ANY_VALUE`, `MODE`, the JSON constructors
    and aggregates (`TO_JSON(ROW(1, 'a'))` is `{"f1":1,"f2":"a"}`) and the
    offset window functions take records. Other functions don't exist for
    one (`function upper(record) does not exist`), nor do arithmetic,
    `LIKE` and casts to types other than text and `RECORD` (`operator does
    not exist: record + bigint`, `cannot cast type record to integer`). A
    record as a condition is `argument of WHERE must be type boolean, not
    type record`
  - **Storage:** as in Postgres, a record can't be stored: a column of a
    table (`CREATE TABLE`, `CREATE TABLE … AS`, `ALTER TABLE … ADD COLUMN`)
    or a view can't have type record (`column "row" has pseudo-type
    record`), and writing one to a column is `column "a" is of type integer
    but expression is of type record`. Arrow structs can't be bound or
    ingested. A table's name as a whole-row value (`SELECT t FROM t`,
    `ROW(t.*)`) isn't supported
  - **Differences from Postgres:** a nested record's fields are known, so
    `((ROW(1, ROW(2, 3))).f2).f1` is 2 (Postgres can't identify the column),
    and records whose fields have different types (`ROW(1)` and `ROW(1.0)`)
    compare by value (Postgres can't compare them). Records of different
    lengths in one column (`UNION ALL`, `CASE`) are an error when they are
    returned, since an Arrow struct has one set of fields
- `ORDER BY … [ASC|DESC] [NULLS FIRST|LAST]`; NULLs sort last by default in
  both directions, except records (see Rows as values)
  - A bare name is an output column's before it is an input column's, as
    in Postgres, also when the output column has no alias, named by its
    expression (see "Column names" above). So after `SELECT CAST(iv AS
    VARCHAR) FROM t`, `ORDER BY iv` sorts the text, not the interval (and
    the index can't sort it); `ORDER BY t.iv`, a position or any other
    expression reads the input.
    Output columns of the same name must be the same expression (`SELECT
    a, a … ORDER BY a`), otherwise it is `ORDER BY "a" is ambiguous`, as
    in `SELECT x::text, x … ORDER BY x`. `DISTINCT ON (…)` names columns
    the same way (`DISTINCT ON "a" is ambiguous`), and so does `ORDER BY`
    on a set operation, with the first branch's columns
  - **Upgrading from v0.0.12 and earlier:** a bare name matched an alias,
    then an input column, so after `SELECT CAST(iv AS VARCHAR)` `ORDER BY
    iv` and `DISTINCT ON (iv)` used the interval, `SELECT x::text, x …
    ORDER BY x` and `SELECT a, b AS a … ORDER BY a` sorted by `x` and `b`,
    and `ORDER BY t.iv` sorted by an alias `iv`. `SELECT DISTINCT CAST(iv
    AS VARCHAR) … ORDER BY iv` was `for SELECT DISTINCT, ORDER BY
    expressions must appear in select list`, and `ORDER BY count` after
    `SELECT COUNT(*)` was `column "count" does not exist` (on a `UNION`,
    `ORDER BY on a UNION result must use output column names or
    positions`)

### Expressions, aggregates, and functions

- Function calls (the functions are listed below) are checked when the
  statement is planned, before any row is read, wherever a call can be:
  the SELECT list, `WHERE`, `GROUP BY`, `HAVING`, `QUALIFY`, `ORDER BY`,
  `ON`, window definitions, subqueries, CTEs and derived tables, the body
  of `CREATE VIEW`, `INSERT`, `UPDATE`, `DELETE`, `MERGE` and `RETURNING`,
  and `CHECK` and `DEFAULT` expressions when they are defined. So the errors
  don't depend on whether a table has rows, and a query that reads none
  (`WHERE false`, dbt's `where false limit 0`, `ExecuteSchema`) gets them
  too:
  - Names are case-insensitive. A quoted name (`"upper"(x)`) or a
    schema-qualified one (`pg_catalog.upper(x)`) is not a call (a syntax
    error), except `"generate_series"(…)` in FROM
  - An unknown function is Postgres's `function nosuchfunc(integer) does
    not exist`, with the argument types the planner infers (`unknown` for
    NULL and for a parameter without a type)
  - A wrong argument count is `UPPER expects 1 argument`, `ROUND expects 1
    or 2 arguments`, `REGEXP_INSTR expects 2 to 7 arguments`, `GREATEST
    expects at least 1 argument` or `RANDOM expects no arguments`, also
    when the arguments are NULL. Only `COUNT` takes `*` (`SUM does not
    accept *`), and only aggregates take `DISTINCT` (`UPPER does not
    accept * or DISTINCT`)
  - Where an argument's type decides whether the call exists, it is
    checked too: `BOOL_OR(int_col)` is `function bool_or(integer) does not
    exist`, and the offset of `LAG` must be an integer
  - As in Postgres, the tables of a statement are resolved before its
    expressions, and an unknown column or function in a call's arguments
    is reported before the call's own error
  - Errors that depend on values (division by zero, a failed cast,
    `LOG(0)`) are still raised when a row is evaluated, as in Postgres
  - A view stored by an earlier version whose body calls an unknown
    function fails whenever it is read, also when its tables are empty:
    `view "v" is no longer valid: function nosuchfunc(integer) does not
    exist`. `CREATE OR REPLACE VIEW` or `DROP VIEW` fixes it
  - A CTE that no part of the statement reads isn't planned, so its calls
    (like its columns) aren't checked
  - `information_schema.routines` lists the functions, each alias too:
    `routine_name` (lower case), `routine_schema` (`pg_catalog`, though a
    call can't name it), `routine_type` (`FUNCTION`), `function_kind`
    (`SCALAR`, `AGGREGATE`, `WINDOW` or `TABLE`), `min_arguments`,
    `max_arguments` (NULL for no maximum) and `alias_of` (the function an
    alias names, NULL otherwise)
- Aggregates, with `GROUP BY` or over the whole input. They skip NULL
  inputs, and all but `COUNT` give NULL when there are none:
  - `COUNT(*)`, `COUNT/SUM/AVG/MIN/MAX(x)`, `COUNT(DISTINCT x)`, and
    `COUNT(DISTINCT a, b, …)` (as in MySQL), which counts the distinct
    combinations of rows where none of them is NULL
  - With `DISTINCT`, values that compare equal count once, as in Postgres
    (see `GROUP BY`): `COUNT(DISTINCT iv)` of `INTERVAL '1 mon'`, `INTERVAL
    '30 days'` and `INTERVAL '1 day'` is 2, and their `SUM(DISTINCT iv)` adds
    the first of the two equal ones read: `1 mon 1 day` (on a cluster, maybe
    `31 days`)
  - Of values that compare equal, `MIN` and `MAX` return the last one, as
    in Postgres: `MAX` of `INTERVAL '1 mon'` and then `INTERVAL '30 days'`
    is `30 days`. Up to v0.0.12 they returned the first one, except as
    window functions
  - `SUM`, `AVG` and the variances and standard deviations add their
    values exactly, whatever their type, also as window functions: every
    integer, decimal and double is a fraction whose denominator is a power
    of 2 times a power of 5, so the driver keeps the sum as such a fraction
    and rounds the result once, to the nearest double (ties to even), or to
    the result's decimal places. So the result is the same in any row
    order: on a cluster, rows come in a different order each time, and
    summing doubles in that order used to give a different last digit from
    one run to the next. Postgres also adds doubles one by one, in its plan's
    order.
    - `SUM` of integers is `BIGINT` and fails (`integer overflow in SUM`)
      only if the total doesn't fit, and `SUM` of decimals is exact at
      their scale. `AVG` (`DOUBLE PRECISION`) is the exact mean, rounded
      once, also for integers beyond 2^53 and for decimals
    - Doubles: a NaN, or both infinities, make `SUM` and `AVG` NaN, and one
      infinity makes them that infinity, as adding them in any order would;
      the variances are then NaN, as in Postgres. A finite sum beyond the
      largest double is an infinity, as before (Postgres raises "value out
      of range: overflow")
    - Adding a value costs an integer addition, about 25 ns for a double
      (40 ns with the squares a variance needs), against 3–7 µs to read a
      row
    - Intervals: `SUM` and `AVG` are `INTERVAL`s, as in Postgres. `SUM` adds
      the months, days and time each on its own, without carrying one into
      the next: `INTERVAL '1 mon'` + `INTERVAL '30 days'` is `1 mon 30
      days`, and 20 and 5 hours are `25:00:00`. `AVG` divides that sum by
      the count as `interval / n` does (see Intervals and date/time
      arithmetic): a fraction of a month becomes days, 30 to a month,
      rounded to millionths of a day, one of a day becomes time, and the
      time is rounded to microseconds, half to even (`AVG` of `1 sec`, `0`
      and `0` is `00:00:00.333333`, as is `INTERVAL '1 sec' / 3`). Each
      field is summed exactly, so only a total out of range is "interval
      out of range", in any row order; Postgres adds the values one by one
      and fails as soon as a running total is out of range, so the same
      rows in another order can fail there. `SUM`'s time part has the
      interval's range (about ±2,562,047 hours, see Intervals and date/time
      arithmetic), while `AVG`'s sum may go up to Postgres's (microseconds
      in 64 bits)
    - **Upgrading from v0.0.12 and earlier:** `SUM` and `AVG` of an
      interval returned a `DOUBLE PRECISION` 0 (so `TO_CHAR(SUM(iv), …)`
      was `expected a date, time or timestamp, got float64`), also as window
      functions
    - **Upgrading:** results of v0.0.7 and earlier can differ in their last
      digits: `SUM`, `AVG` and the variances of doubles, `AVG` of decimals,
      and `AVG` of integers whose sum is beyond 2^53. The new value is the
      correctly rounded one. `SUM` of integers no longer fails when a running
      total overflows and the final one fits, and a decimal `SUM` whose
      values include integers (`SUM(CASE WHEN … THEN 1 ELSE 2.5 END)`) no
      longer leaves the integers out
  - `STRING_AGG(x, sep [ORDER BY …])`, and `LISTAGG(x [, sep]) [WITHIN GROUP
    (ORDER BY …)]` (Snowflake, Oracle; `sep` defaults to `''`);
    `STRING_AGG(x, sep) WITHIN GROUP (ORDER BY …)` also works (SQL Server).
    Values of any type are joined as text. As in PostgreSQL, each value but
    the first is preceded by its own row's separator, and with `DISTINCT`
    (whose `ORDER BY` may only use the arguments) the values come out sorted
  - `BOOL_OR(b)`, `BOOL_AND(b)` / `EVERY(b)` on booleans, `ANY_VALUE(x)` (a
    non-NULL value of the group)
  - `VAR_SAMP` / `VARIANCE`, `VAR_POP`, `STDDEV_SAMP` / `STDDEV`,
    `STDDEV_POP` return `DOUBLE PRECISION` (PostgreSQL returns `numeric` for
    integer and numeric inputs). The values and their squares are summed
    exactly, so the result is the correctly rounded double whatever the
    values' type and size. The sample versions are NULL for one value, the
    population ones 0
  - Ordered-set aggregates: `PERCENTILE_CONT(f) WITHIN GROUP (ORDER BY x)`
    interpolates (numbers as doubles, and intervals), `PERCENTILE_DISC(f)
    WITHIN GROUP (ORDER BY x)` returns the first value at or past fraction
    `f` (any sortable type), `MODE() WITHIN GROUP (ORDER BY x)` the most
    frequent value (the first in sort order among ties), with PostgreSQL's
    formulas and errors. `MEDIAN(x)` is `PERCENTILE_CONT(0.5) WITHIN GROUP
    (ORDER BY x)`. The fraction is evaluated once per group (a constant, a
    parameter or a grouped column) and must be between 0 and 1
    - Between two intervals `lo` and `hi`, `PERCENTILE_CONT` is `lo + (hi -
      lo) * p`, `p` being how far the position is from `lo` to `hi`, with
      `interval * n` (see Intervals and date/time arithmetic), as in
      Postgres: `MEDIAN` of `1 year` and `1 mon 1 day 1 sec` is `6 mons 16
      days -11:59:59.5`. The difference's months and days must fit in 32
      bits ("interval out of range"), but its time needn't fit an
      interval's nanoseconds: `MEDIAN` of `-2000000 hours` and `2000000
      hours` is `00:00:00`
    - **Upgrading from v0.0.12 and earlier:** the interpolation cascaded
      fractions in nanoseconds (that `MEDIAN` was `6 mons 15 days
      12:00:00.5`), and a difference past ±2,562,047 hours was "interval
      out of range"
  - `agg(…) FILTER (WHERE cond)` on any aggregate, also as a window function:
    only the rows where `cond` is true are aggregated, and their arguments
    alone are evaluated
  - Aggregate `ORDER BY` matters only for `STRING_AGG` / `LISTAGG`; its NULLs
    sort last by default in either direction, as in `ORDER BY`
- Window functions, `fn(…) OVER ([PARTITION BY …] [ORDER BY …] [frame])`, in
  the SELECT list, `ORDER BY` and `QUALIFY`. They see the rows after `WHERE`,
  `GROUP BY` and `HAVING`, and work anywhere a query does (joins, subqueries,
  CTEs, views, CTAS, `INSERT … SELECT`):
  - Ranking: `ROW_NUMBER`, `RANK`, `DENSE_RANK`, `PERCENT_RANK`, `CUME_DIST`,
    `NTILE(n)`
  - Offset: `LAG` / `LEAD(x [, offset [, default]])`, `FIRST_VALUE(x)`,
    `LAST_VALUE(x)`, `NTH_VALUE(x, n)`, each with `IGNORE NULLS` or `RESPECT
    NULLS` (the default) after the call (SQL standard, Snowflake) or after
    its last argument (BigQuery, DuckDB). `IGNORE NULLS` counts only the rows
    whose value is not NULL, so `LAST_VALUE(x) IGNORE NULLS OVER (ORDER BY
    ts)` fills forward; `LAG(x, 0)` is still the current row's value
  - Aggregates: `COUNT(*)`, `COUNT/SUM/AVG/MIN/MAX(x)`, `STRING_AGG`,
    `LISTAGG`, `BOOL_OR`, `BOOL_AND`, `EVERY`, `ANY_VALUE`, and the variances
    and standard deviations, with `FILTER (WHERE …)`, also over grouped
    results (`SUM(COUNT(*)) OVER (ORDER BY day)`). As in PostgreSQL,
    `DISTINCT`, an aggregate `ORDER BY` (order the window instead) and the
    ordered-set aggregates and `MEDIAN` are not supported with `OVER`
  - Frames: `{ROWS | RANGE | GROUPS} {start | BETWEEN start AND end}` with
    `UNBOUNDED PRECEDING`, `n PRECEDING`, `CURRENT ROW`, `n FOLLOWING`,
    `UNBOUNDED FOLLOWING`, and `EXCLUDE CURRENT ROW | GROUP | TIES | NO
    OTHERS` (`GROUP` removes the current row and its peers, `TIES` only its
    peers). `RANGE` offsets are numbers for a numeric `ORDER BY` key and
    intervals for dates, times, timestamps and intervals; as in PostgreSQL,
    a `TIME` key moves by the interval's time part and its frames don't wrap
    around midnight. Without a frame, the SQL default applies: with `ORDER
    BY`, from the start of the partition to the current row's last peer (rows
    with equal keys); without `ORDER BY`, the whole partition
  - Named windows: `WINDOW w AS (…)`, `OVER w`, `OVER (w ORDER BY … [frame])`
  - `QUALIFY` filters on window results and may use output aliases
    (`QUALIFY ROW_NUMBER() OVER (PARTITION BY k ORDER BY ts DESC) = 1`)
  - Result types: `BIGINT` for `ROW_NUMBER`, `RANK`, `DENSE_RANK`, `NTILE` and
    `COUNT`; `DOUBLE PRECISION` for `PERCENT_RANK`, `CUME_DIST` and `AVG`
    (`INTERVAL` for intervals); the argument's type for `MIN`, `MAX` and the
    offset functions; the other aggregates as with `GROUP BY`
  - As in `ORDER BY`, NULLs sort last by default in either direction
    (PostgreSQL puts them first for `DESC`). Rows that tie on the window's
    `ORDER BY` keep their input order
- Dates, times and timestamps become text (`CAST(x AS VARCHAR)`, `||`,
  `CONCAT`, `STRING_AGG`, `LIKE`, `MD5(CAST(x AS TEXT))`, …) as Postgres
  writes them, whatever the declared precision: seconds always, then the
  fraction without trailing zeros (`2024-01-10 10:00:00`,
  `2024-01-10 10:00:00.5`, `10:00:00.05`), a timestamp with time zone as
  its local time in the session time zone with that zone's offset
  (`2024-01-10 10:00:00+00` in UTC, `2024-01-10 02:00:00-08` in
  America/Los_Angeles; see Session time zone), a year before 1 AD as a BC
  year (`0044-03-15 BC`), and a year past 9999 with all its digits
  (`10000-01-01`). Text in those forms reads back as the same value, and
  there is no year 0. JSON (`TO_JSON`, `JSON_BUILD_OBJECT`, …) has the ISO
  8601 form, `2024-01-10T10:00:00.5+00:00`, also in the session time zone.
  Intervals are written as in Postgres too, with a `+` on a field after a
  negative one (`-1 days +02:00:00`). Query results are Arrow values, which
  this doesn't change
  - **Upgrading from v0.0.7 and earlier:** they kept the declared
    precision's trailing zeros (`2024-01-10 10:00:00.000000`, `10:00:00.000`,
    `2024-01-10 10:00:00.000+00`), wrote years before 1 AD as `0000-…` or
    `-0001-…`, and left out an interval's `+`. The text of such values
    changes, and with it every hash of the text: dbt_utils'
    `generate_surrogate_key` over a timestamp or time column, and the
    `dbt_scd_id` of dbt snapshots (an `md5` of the key and `updated_at` as
    text). Keys that earlier versions stored don't match the ones computed
    now (which match Postgres's): an incremental model whose `unique_key`
    is such a key inserts its rows again instead of updating them, and a
    snapshot whose `unique_key` is one sees every row as new. Rebuild those
    (`dbt run --full-refresh`, or drop the snapshot's table and run `dbt
    snapshot`) before running them with this version. Snapshots only read
    their stored `dbt_scd_id`s back, so they keep working; the rows they
    add from now on have ids in the new form. Text stored by earlier
    versions (`CAST(ts AS VARCHAR)` written to a column) keeps the old form,
    and still reads back as the same values, except for years before 1 AD
- Casts: `CAST(x AS type)` and `x::type` fail on a value that doesn't
  convert (text that doesn't parse, a value out of the type's range,
  overflow). `TRY_CAST(x AS type)` and `SAFE_CAST(x AS type)` return NULL
  for it instead, and `CAST(x AS type DEFAULT v ON CONVERSION ERROR)` returns
  `v` (converted to the type). The result has the target type. An error
  while computing `x`, and a cast between types that never convert (`DATE`
  to `BOOLEAN`), are still errors. A value that doesn't convert fails with
  `InvalidData` and a class `22` SQLSTATE, and a cast between types that
  never convert with `InvalidArgument` and `42846` (see
  [Error handling](#error-handling)). A
  cast to `VARCHAR(n)` or `CHAR(n)` cuts the text to `n` characters, and
  `CHAR(n)` pads it (see String lengths). A
  cast to a `TIME(p)` or `TIMESTAMP(p)` of a lower precision rounds to `p`
  digits (see Fractional seconds under Types), and casts between `TIMESTAMP`,
  `TIMESTAMP WITH TIME ZONE`, `DATE`, `TIME` and text use the session time
  zone (see Session time zone)
  - **Text as an integer** (`SMALLINT`, `INTEGER`, `BIGINT`) is read as
    Postgres's `int4in` reads it: optional spaces and a sign, then digits,
    with single underscores between them (`'1_000'`), or `0x`, `0o` or
    `0b` and hexadecimal, octal or binary digits (`'0x1F'` is 31). Anything
    else (`'1.5'`, `'1e3'`, `'1.0'`, `''`) is `invalid input syntax for
    type integer: "1.5"` (`22P02`), and a value past the type's range is
    `value "3000000000" is out of range for type integer` (`22003`), with
    `smallint` or `bigint` for those types. Text is read so wherever it
    becomes an integer: written to an integer column (`INSERT`, `UPDATE`,
    `MERGE`, `DEFAULT`s, bound parameters, bulk ingest and `redis-adbc
    copy`), compared with an integer (`WHERE n = '1.5'` fails), and as a
    function's integer argument (`LPAD(s, '3.5')` fails, as an `INTEGER`).
    Text compared with a value of another type it doesn't convert to fails
    with that type's conversion error and SQLSTATE (`DATE '2024-01-01' =
    'x'` is `invalid date "x"`, `22007`)
  - Differences: an integer literal is a `BIGINT`, so `1 = 'x'` says `type
    bigint` where Postgres says `type integer`. An `IN` list stops at the
    first value that matches, so `1 IN ('1', '1.5')` is true, and an
    indexed column's `IN` list skips the values that don't convert; Postgres
    reads every value first, and fails on `'1.5'`. A text column compared
    with an integer is read row by row (`invalid input syntax for type
    integer: "a"` for a row of `'a'`); Postgres refuses the comparison
    (`operator does not exist: text = integer`)
  - **Upgrading from v0.0.12 and earlier:** text was read as a number and
    rounded, so `CAST('1.5' AS INTEGER)` was 2, `'1e3'` was 1000 and `'1.0'`
    was 1, also when written to a column, and `WHERE n = '1.5'` matched the
    rows where `n` is 2; `'0x1F'` and `'1_000'` were errors. The errors were
    `invalid decimal "x"`, `value 3000000000 out of range for INTEGER` and
    `cannot convert decimal value "99999999999999999999" to BIGINT`, and
    text compared with a value it didn't convert to was `cannot compare
    int32 with string`, without a SQLSTATE. `redis-adbc copy -type
    n=INTEGER` rounded a field of `1.5`; it now stops there, or copies NULL
    with `-on-error null`
- Math functions: `ROUND(x [, n])` and `TRUNC(x [, n])` (`n` may be
  negative: `ROUND(1250, -2)` is 1300), `FLOOR`, `CEIL` / `CEILING`, `MOD` / `%`,
  `POWER` / `POW`, `SQRT`, `CBRT`, `LN`, `LOG(x)` (base 10) / `LOG(b, x)`,
  `LOG10`, `EXP`, `SIGN`, `ABS`, `PI()`, `RANDOM()`
  - `ROUND` rounds half away from zero: exactly on `NUMERIC`, and on
    `DOUBLE PRECISION` too, by the value as written (`ROUND(2.675e0, 2)` is
    2.68). Postgres rounds doubles half to even
  - Integers and doubles keep their type. `ROUND(NUMERIC(p,s), n)` has scale
    `n` (at most `s`); `ROUND(x)`, `TRUNC(x)`, `FLOOR` and `CEIL` scale 0.
    `MOD` and `%` have their arguments' common type (`NUMERIC` stays
    exact); `SQRT`, `CBRT`, `LN`, `LOG`, `EXP`, `POWER`, `PI` and `RANDOM`
    return `DOUBLE PRECISION`
  - Errors as in Postgres for the square root of a negative number, the
    logarithm of zero or of a negative number, `MOD` / `%` by zero (of any
    numeric type), zero to a negative power, and overflow. `EXP` of a double
    that underflows is an error too ("value out of range: underflow"); `EXP`
    of a `NUMERIC` then returns 0, as Postgres's `exp(numeric)` does
- Trigonometric functions, as in Postgres, all `DOUBLE PRECISION`:
  - In radians: `SIN`, `COS`, `TAN`, `COT`, `ASIN`, `ACOS`, `ATAN`,
    `ATAN2(y, x)`; `RADIANS(degrees)` and `DEGREES(radians)` convert
  - In degrees: `SIND`, `COSD`, `TAND`, `COTD`, `ASIND`, `ACOSD`, `ATAND`,
    `ATAN2D(y, x)`. With Postgres's algorithm, they are exact where the
    result is a simple number: `SIND(30)` is 0.5, `COSD(60)` 0.5, `TAND(45)`
    1, `ASIND(0.5)` 30, `ATAN2D(1, 1)` 45, and `TAND(90)` and `COTD(0)` are
    Infinity
  - Arguments are numbers (or text that converts); a NULL argument gives
    NULL and a NaN gives NaN. Errors as in Postgres: `ASIN: input is out of
    range` for an argument beyond [-1, 1] of `ASIN`, `ACOS`, `ASIND` and
    `ACOSD`, and for an infinite one of the sines, cosines, tangents and
    cotangents; `DEGREES` can overflow and `RADIANS` underflow ("value out of
    range: …")
  - The radian functions are Go's `math` package. Postgres uses the
    platform's C library, which can differ in the last bit: `TAN(1)` is
    1.557407724654902 here and 1.5574077246549023 with glibc
  - So dbt_utils' `haversine_distance` works, in its default form (with
    `radians`) and in its BigQuery form (with `acos(-1)`)
- String functions; positions are 1-based and count characters, not bytes:
  - `SUBSTRING(s, start [, len])`, `SUBSTRING(s FROM start [FOR len])`
    (with text arguments, the pattern forms below),
    `SUBSTR`, `LEFT(s, n)` / `RIGHT(s, n)` (a negative `n` drops characters
    from the other end), `POSITION(sub IN s)`, `STRPOS(s, sub)`
  - `TRIM([BOTH | LEADING | TRAILING] [chars] FROM s)`, `TRIM(s [, chars])`,
    `LTRIM`, `RTRIM`, `BTRIM` (spaces by default), `LPAD` / `RPAD(s, len
    [, fill])`, `REPLACE`, `REVERSE`, `REPEAT`, `INITCAP`, `MD5`,
    `STARTS_WITH`, `SPLIT_PART(s, delim, n)` (a negative `n` counts from
    the end)
  - `REGEXP_REPLACE(s, pattern, replacement [, start [, n]] [, flags])`
    replaces the first match, or all of them with flag `g`; with Postgres
    16's `start` and `n`, the `n`-th match from `start` (all of them if `n`
    is 0). `\1` … `\9` and `\&` in the replacement insert groups and the
    match. Flags and pattern syntax as below
- Regular expressions, with Postgres's arguments, results and errors:
  - `s ~ pattern`, `s ~* pattern` (case-insensitive), `s !~ pattern`,
    `s !~* pattern`. As in Postgres, they bind like `||`: tighter than
    comparisons, `IS`, `LIKE`, `BETWEEN` and `IN`, looser than arithmetic
    (`s ~ 'a' || 'b'` is `(s ~ 'a') || 'b'`)
  - `REGEXP_LIKE(s, pattern [, flags])`, `REGEXP_COUNT(s, pattern [, start
    [, flags]])`, `REGEXP_INSTR(s, pattern [, start [, n [, endoption
    [, flags [, subexpr]]]]])` and `REGEXP_SUBSTR(s, pattern [, start [, n
    [, flags [, subexpr]]]])`, as in Postgres 15 (Snowflake's have the same
    order): the `n`-th match from position `start`, or its group `subexpr`.
    A search from `start` sees the text before it, so `^` does not match
    there
  - `REGEXP_MATCH(s, pattern [, flags])` returns the first match's groups
    (or the whole match if there are none) as the text of Postgres's
    `text[]` result, `'{bar,beque}'`, since the driver has no array type.
    `REGEXP_SUBSTR(s, pattern, 1, 1, '', n)` returns group `n` alone
  - `SUBSTRING(s FROM pattern)` returns the first group of the first match,
    or the whole match. As in Postgres, the argument types choose the form:
    text is a pattern and an integer a position, so `SUBSTRING(s, '2')` is a
    pattern match and `SUBSTRING(s FROM 2 FOR 3)` a position
  - `[NOT] SIMILAR TO pattern [ESCAPE e]` and `SUBSTRING(s SIMILAR pattern
    ESCAPE e)` (also `SUBSTRING(s FROM pattern FOR e)`) translate the SQL
    pattern as Postgres does. It must match the whole string, `%` and `_`
    are wildcards, `|`, `*`, `+`, `?`, `{m,n}`, `( )` and `[ ]` keep their
    meaning, and the escape character (backslash by default, none with
    `ESCAPE ''`) makes the next character literal (an escaped letter is an
    RE2 escape, such as `\d` for a digit). `SUBSTRING … SIMILAR`
    returns the part between the two `e"` separators, where the part before
    them matches as little of the string as it can
  - Flags: `i` (case-insensitive), `c`, `n` / `m` (newline-sensitive), `s`,
    `p`, `w`, `q` (a literal pattern), and `g` for `REGEXP_REPLACE` only;
    `x`, `b` and `e` are not supported. Without flags, `.` matches newlines
    and `^` and `$` anchor the whole string
  - Patterns use Go's RE2 syntax: no backreferences (`\1`) or lookaround,
    and `\b` is a word boundary (`\y` in Postgres). Where a pattern can match
    text of different lengths at the same position, RE2 takes the first
    alternative, as Perl does, so `SUBSTRING('xyz' FROM 'x|xy')` is `x`
    where Postgres gives `xy`. A pattern that doesn't compile is an error
    such as `` invalid regular expression: missing closing ): `(` ``
  - Patterns over columns are evaluated by the driver, on the rows the rest
    of the `WHERE` clause finds in the index: `~ '^abc'` and
    `SIMILAR TO 'abc%'` are not index prefix queries. `REGEXP_MATCHES` and
    `REGEXP_SPLIT_TO_TABLE` / `REGEXP_SPLIT_TO_ARRAY` (sets and arrays) are
    not supported
- Conditional functions: `NULLIF(a, b)`, `GREATEST(…)` / `LEAST(…)` (NULL
  arguments are ignored), and `IIF(cond, a, b)` (like `CASE`, only the chosen
  branch is evaluated). `COALESCE`, `GREATEST` and `LEAST` widen their
  arguments to a common type (`COALESCE(int_col, 2.5)` is `NUMERIC`); a
  string literal (see below), or a number that type holds exactly, takes the
  other arguments' type (`COALESCE(int_col, 0)` stays `INTEGER`,
  `GREATEST(d, '2024-01-01')` is a `DATE`)
- Quoted string literals (`'1'`, `$$1$$`) have no type of their own, as in
  Postgres (its `unknown`): where an expression's value is one of several
  values, a literal among them takes the type of the others, and is text if
  all are. So `CASE WHEN id = 1 THEN '1' ELSE 1 END` is a `BIGINT`, whose
  `'1'` is the integer 1: arithmetic, sorting, grouping, `DISTINCT`, window
  partitions and index queries see integers. These are the results of
  `CASE` and `IIF`, the arguments of `COALESCE`, `GREATEST` and `LEAST`,
  `LAG`'s and `LEAD`'s default, `NULLIF`'s arguments (`NULLIF('1', int_col)`
  is an `INTEGER`), and a column of the branches of `UNION`, `INTERSECT`
  and `EXCEPT` (`SELECT '1' UNION SELECT 1` is a `BIGINT`), also in `CREATE
  TABLE … AS` and views. A cast gives a literal a type (`CAST('1' AS
  VARCHAR)`, `'1'::text`). An operand of `+`, `-`, `*`, `/` and `%`
  (`MOD`) takes its type from the operator and the other operand, as
  Postgres resolves the operator: a value of the other operand's type if
  the operator takes two (`'1' + 1` is 2, `id + '1'` adds 1, `d -
  '2024-01-01'` is a number of days, `ts - '2024-01-01'` an interval), and
  otherwise the type of the one form the operator has for it (`ts + '1
  day'` and `tm + '01:00'` add an interval, `iv * '2'` multiplies by a
  double), also in `WHERE` (`id = '1' + 1` is an index query), `GROUP BY`,
  `UPDATE … SET` and views. A `NUMERIC` literal keeps its own digits (`n *
  '2'` has `n`'s scale).
  - **Errors,** as in Postgres, when the statement is planned, also if no
    row reads the value: a literal that isn't a value of the type (`CASE
    WHEN … THEN 'x' ELSE 1 END` and `'x' + 1` are `invalid decimal "x"`;
    Postgres: `invalid input syntax for type integer: "x"`; both `22P02`);
    typed values without a common type, such as a `VARCHAR` column and an
    integer (`CASE WHEN … THEN v ELSE 1 END` is `CASE types bigint and
    varchar cannot be matched`, `42804`, and so for `COALESCE`, `GREATEST`,
    `LEAST` and `IIF`; a `CASE` of literals alone is text, so
    `COALESCE(CASE WHEN … THEN '1' END, 1)` is such an error too); and an
    operator that has no form for the other operand's type (`b + '1'` is
    `operator does not exist: boolean + unknown`, `42883`) or more than one
    (`d + '1'` could add days or an interval: `operator is not unique: date
    + unknown`, `42725`), as are two literals (`'1' + '1'`, `NULL + '1'`)
    and `-'1'`. For `MOD(…)` they are `function mod(date, unknown) does not
    exist` and `function mod(unknown, unknown) is not unique`.
  - **Differences:** the literal is read as `CAST` reads text, so `'1.5'`
    as an integer is 2 where Postgres fails (`'1.5' + 1` is 3). A `CASE`
    whose results are all `NULL` has no type, as `NULL` has, so
    `COALESCE(CASE WHEN … THEN NULL END, '2', 1)` is 2 (Postgres's `CASE`
    is text: `COALESCE types text and integer cannot be matched`). `%` and
    `MOD` of a `DOUBLE PRECISION` are the driver's (Postgres has none), and
    so is `f % '1'`; `tm * '2'` fails as `tm * 2` does (Postgres reads the
    time as an interval); `+'1'` stays text (Postgres: a double).
    Comparisons read text as the other side's type, and `INSERT`, `UPDATE`
    and casts convert it to theirs, as before.
  - **Upgrading from v0.0.12 and earlier:** results change type. Such a
    `CASE`, `IIF`, `NULLIF` or `LAG` was a `VARCHAR`, whose other values
    weren't converted (`CASE WHEN id = 1 THEN '1' ELSE 1 END` gave the text
    `'1'` and the integer 1), so its column, and the column `CREATE TABLE …
    AS` made of it, is now a `BIGINT` (or the other values' type), `+ 1` of
    it works, and `COUNT(DISTINCT …)`, `DISTINCT` and window partitions see
    one value where they saw two. `COALESCE`, `GREATEST` and `LEAST` had
    the type already, but gave a literal's text where the other arguments
    were NULLs without a type (`COALESCE(CASE WHEN … THEN n END, '0')` gave
    `'0'`, so `+ 1` of it failed), and `GREATEST('1', '2', 3)` and `SELECT
    '1' UNION SELECT 1` were `types VARCHAR and BIGINT cannot be matched`.
    Typed values without a common type gave a `VARCHAR` column of values of
    both types, and are now an error: cast them (`CAST(n AS VARCHAR)`).
    Arithmetic of a literal was an error (`operator + is not supported for
    string and int64`, or `for DATE and VARCHAR`, without a SQLSTATE), and
    works now, or fails with Postgres's error (`42883`, `42725`)
- Functions return NULL for a NULL argument, except `COALESCE`, `NULLIF`,
  `GREATEST`, `LEAST` and `IIF`. Functions of constants are computed once,
  so `WHERE n > ROUND(?)` is still an index query; functions over columns are
  evaluated by the driver on the rows the index returns. `RANDOM()` is
  computed for every row and never pushed down
- Date/time functions. A `TIMESTAMP WITH TIME ZONE`'s fields are those of
  its local time in the session time zone (see Session time zone), and the
  current time is fixed once per statement:
  - `CURRENT_DATE`, `CURRENT_TIMESTAMP` / `NOW()`, `CURRENT_TIME`,
    `LOCALTIMESTAMP`, `LOCALTIME` (the date and times are local), read in
    microseconds, as Postgres's clock is. With a precision,
    `CURRENT_TIMESTAMP(p)`, `LOCALTIMESTAMP(p)`, `CURRENT_TIME(p)`,
    `LOCALTIME(p)` and `NOW(p)` are rounded to `p` digits, as in Postgres
    (`CURRENT_TIMESTAMP(0)` is whole seconds, and `CURRENT_TIMESTAMP(2)` is
    `CAST(CURRENT_TIMESTAMP AS TIMESTAMPTZ(2))`), and have type
    `TIMESTAMP(p) WITH TIME ZONE`, `TIMESTAMP(p)`, `TIME(p) WITH TIME ZONE`
    (`CURRENT_TIME`, up to 6 digits) or `TIME(p)` (`LOCALTIME`); `p` is an
    integer constant from 0 to 9. `CURRENT_TIME` is the local time with the
    session time zone's offset (`10:30:00-04`), as in Postgres. Up to
    v0.0.12 it was a `TIME`, an Arrow `time64`; it is now a string column
    (see Types)
  - `EXTRACT(field FROM x)` / `DATE_PART('field', x)` for `year`, `isoyear`,
    `quarter`, `month`, `week` (ISO), `day`, `dow` (0 = Sunday), `isodow`,
    `doy`, `hour`, `minute`, `second`, `milliseconds`, `microseconds`, `epoch`,
    `decade`, `century`, `millennium`, and for a timestamp with time zone
    or a time with time zone `timezone`, `timezone_hour` and
    `timezone_minute` (its offset, east of UTC). A time with time zone has
    the time's fields, `epoch` (the seconds of its time in UTC, so
    `'10:00+02'` is 28800) and the offset's; the others are `unit "day" not
    supported for type time with time zone`, as in Postgres. Shortcuts
    `YEAR()`, `QUARTER()`, `MONTH()`, `WEEK()`, `DAY()`, `DAYOFYEAR()`,
    `HOUR()`, `MINUTE()`, `SECOND()`
  - `DATE_TRUNC('unit', x)` (dates stay dates), `DATE_DIFF('unit', a, b)`
    (unit boundaries crossed), `LAST_DAY(d)`
  - `DATE_BIN(stride, source, origin)` (Postgres 14): the start of the
    `stride`-long bin, counted from `origin`, that `source` is in, also when
    `origin` is after it. The stride is elapsed time (a day is 24 hours) and
    has no months or years (`timestamps cannot be binned into intervals
    containing months or years`). It is a `TIMESTAMP` if the source or the
    origin is one and neither has a time zone, otherwise a `TIMESTAMP WITH
    TIME ZONE`, as Postgres picks its form
  - `DATEADD(part, n, x)` and `DATEDIFF(part, a, b)` (Snowflake, Redshift,
    SQL Server; what dbt's `dateadd` and `datediff` emit), with `part` a bare
    keyword or a string: `year`, `quarter`, `month`, `week`, `day`, `hour`,
    `minute`, `second`, `millisecond`, `microsecond`, their plurals, and
    abbreviations such as `yy` / `yyyy`, `qq`, `mm` / `mon`, `wk` / `ww`,
    `dd` / `d`, `hh`, `mi` / `n` / `m`, `ss` / `s`, `ms`, `us` / `mcs` (`m`
    is minute and `w` is week, as in Snowflake). In the part's position a
    bare part name is the part even if a column has that name (write `"day"`
    for the column); everywhere else it is the column
  - `DATEDIFF` counts boundaries crossed, exactly like `DATE_DIFF`.
    `DATEADD` adds `n` parts (a non-integer `n` is rounded, as by
    `CAST(n AS BIGINT)`); months clamp to the end of the month. As in
    Snowflake, a date stays a date for a part of a day or longer and becomes
    a timestamp for a smaller one; timestamps and times keep their type
    (times wrap around midnight, and date parts aren't valid for them), and
    text is read as a timestamp
  - Aliases: `TIMESTAMPADD` / `TIMESTAMPDIFF` (as in Snowflake; MySQL's
    `TIMESTAMPDIFF` counts whole units elapsed instead),
    `DATE_ADD(part, n, x)` (Trino, Databricks), and
    `DATE_ADD(x, INTERVAL n part)` / `DATE_SUB(x, INTERVAL n part)` (MySQL,
    BigQuery), which are `DATEADD(part, n, x)` / `DATEADD(part, -n, x)`
  - `MAKE_DATE`, `MAKE_TIME`, `MAKE_TIMESTAMP`, `MAKE_TIMESTAMPTZ`,
    `TO_TIMESTAMP(epoch_seconds)`, `EPOCH(x)`, `EPOCH_MS(x)`. As in
    Postgres, a negative year is BC (`MAKE_DATE(-44, 3, 15)` is
    `0044-03-15 BC`), and year 0 is an error (`date field value out of
    range: 0-03-15`). Up to v0.0.12 they counted years astronomically, so
    -44 was 45 BC and 0 was 1 BC
  - `TO_CHAR(x, format)` with Postgres's template patterns, read as
    Postgres reads them (case-sensitive, the first pattern that matches):
    `HH` / `HH12`, `HH24`, `MI`, `SS`, `MS`, `US`, `FF1` to `FF6`, `SSSS` /
    `SSSSS`, `AM` / `PM` / `A.M.` / `P.M.`, `Y,YYY`, `YYYY`, `YYY`, `YY`,
    `Y`, `IYYY`, `IYY`, `IY`, `I`, `BC` / `AD` / `B.C.` / `A.D.`, `MONTH` /
    `Month` / `month`, `MON` / `Mon` / `mon`, `MM`, `DAY` / `Day` / `day`,
    `DY` / `Dy` / `dy`, `DDD`, `IDDD`, `DD`, `D`, `ID`, `W`, `WW`, `IW`,
    `CC`, `J`, `Q`, `RM` / `rm`, `TZ` / `tz`, `TZH`, `TZM` and `OF`. The
    numbers and the meridiem and era indicators can also be written in
    lower case (`yyyy-mm-dd`, `pm`).
    - Modifiers: `FM` before a pattern (no padding), `TM` (names without
      padding; they are English), and `TH` / `th` after a number (`DDth` is
      `29th`). `FX`, and `SP` after a pattern, are accepted and ignored, as
      in Postgres
    - Names are padded to 9 characters and `RM` to 4 (`'XI  '`), numbers
      with zeros, unless `FM` is given; `MS`, `US` and `FF1` to `FF6` are
      always padded. `CC` is the century that starts in a year ending in
      01, and `-01` for the 1st century BC. Years before 1 AD are their BC
      years, for `BC` (`0044 BC`)
    - A timestamp with time zone is formatted as its local time in the
      session time zone, and a date as local midnight: `TZ` is that zone's
      abbreviation there (`UTC`, `PST` / `PDT`, `IST`; `tz` in lower case),
      and `TZH`, `TZM` and `OF` its offset (`-08`, `00` and `-08`; `+00`,
      `00` and `+00` in UTC). For a timestamp `TZ` is empty and the offset
      `+00`
    - Anything else is copied: text in double quotes (where `\` escapes the
      next character), `\"` (a `"`), and every character that doesn't start
      a pattern, so `Mm` is `Mm` while `Dd` is two weekday numbers, as in
      Postgres. An empty format gives NULL
    - An interval is formatted from its own fields, as in Postgres: `YYYY`
      is its years, `MM` the months left over (`TO_CHAR(INTERVAL '14 mons',
      'YYYY MM')` is `0001 02`), `DD` its days and `HH24` its hours, which
      can pass 23. `HH` / `HH12` are on a 12-hour clock, and `AM` / `PM`
      follow the hour of the day. A negative interval's fields are negative,
      and numbers are padded one wider for the sign (`YYYY` is `-0001`, `MM`
      `-02`). `CC` is the years / 100, `DDD` the length in days with a month
      as 30, `Q` and `RM` come from the months (empty without them), and
      `J`, `IYYY`, `IW` and `IDDD` are Postgres's date arithmetic on the
      fields. The patterns that need a calendar date (`MONTH`, `MON`, `DAY`,
      `DY`, `D`, `ID`, `BC` / `AD`, `TZ`, `TZH`, `TZM` and `OF`) are an
      error, as in Postgres: `invalid format specification for an interval
      value`
    - A time is formatted as the interval since midnight, because Postgres
      casts it to one: `TO_CHAR(TIME '13:05', 'YYYY-MM-DD HH24:MI')` is
      `0000-00-00 13:05`. The session time zone doesn't change it. Postgres
      has no `TO_CHAR` of a time with time zone, so it is `function
      to_char(time with time zone, varchar) does not exist`
    - **Upgrading from v0.0.12 and earlier:** `TO_CHAR` of an interval was
      an error (`expected a date, time or timestamp, got interval`), and a
      time was formatted as on 1970-01-01: `YYYY` was `1970`, `J` `2440588`
      and `Day` `Thursday`, and `TZ`, `D` and the other patterns that need a
      calendar date gave a value
  - `TO_DATE(text, format)` and `TO_TIMESTAMP(text, format)` read the same
    templates as Postgres 16 does: every pattern above but `TZ`, `tz` and
    `OF`, which are an error (`formatting field "TZ" is only supported in
    to_char`, as in Postgres 16). `TO_TIMESTAMP` reads the text as a local
    time in the session time zone, unless `TZH` and `TZM` give its offset.
    `TO_DATE` ignores the time and the offset
    - **Reading:** a number is as wide as its pattern when another number
      follows it (`YYYYMMDD` reads `20240315`); otherwise, or with `FM` or
      `TH`, it takes all its digits and a sign (`-44` for `YYYY`). Names are
      read in any case. Outside `FX` mode, spaces before and after a field
      are skipped, a separator or space in the format matches one in the
      text or none (`TO_DATE('2024/02', 'YYYY-MM-DD')` is 2024-02-01), and
      any other character, also in quotes, skips one character. Text left
      after the format is ignored
    - **Combining fields:** a field that isn't given, or is 0, is year 0
      (1 BC), January, the 1st or midnight, so `TO_DATE('03-15', 'MM-DD')`
      is `0001-03-15 BC`. `YYY`, `YY` and `Y` are years near 2020 (`69` is
      2069, `70` 1970), or with `CC` years of that century; a negative year
      or `BC` is BC. `J` gives the date (with `DD` and `MM` replacing its
      day and month), `WW` and `W` the first day of their week, `DDD` the
      month and the day not given, and `IYYY` with `IW`, `ID` or `IDDD` an
      ISO week date. `D`, `DAY`, `DY` and `Q` are read and otherwise
      ignored, but `DAY` and `DY` are the day of an `IW` week. `MS`, `US`
      and `FF1` to `FF6` are fractions of a second (`.5` is 500 ms), and
      `FFn` rounds to n digits
    - **Errors** are Postgres's messages, without its details and hints: a
      field given twice with different values (`conflicting values for "MM"
      field in formatting string`), Gregorian and ISO week patterns together
      (`invalid combination of date conventions`), text a pattern can't read
      (`invalid value "xx" for "MM"`), and fields out of range
      (`date/time field value out of range: "2024-02-30"`). The results
      have the driver's date and timestamp ranges (below), which go further
      back than Postgres's 4714 BC
    - **Upgrading from v0.0.12 and earlier:** they read only `YYYY` (also
      `IYYY`), `YY`, `MM`, `MONTH` / `MON`, `DD`, `HH24`, `HH12` / `HH`,
      `MI`, `SS`, `MS`, `US` and `AM` / `PM`, unlike Postgres: a missing year
      was 1970, `MS` and `US` read a count (`.5` was 5 ms), `MON` and `MONTH`
      took both forms, and text after the format, or a missing separator,
      was an error
  - Time zones, as in Postgres: `x AT TIME ZONE zone` (also
    `TIMEZONE(zone, x)`) reads a `TIMESTAMP` as a local time in `zone` and
    gives that instant, a `TIMESTAMP WITH TIME ZONE`, and gives the local
    time in `zone` of a `TIMESTAMP WITH TIME ZONE`, a `TIMESTAMP`. A `DATE`
    and text are read as a `TIMESTAMP WITH TIME ZONE` (local times in the
    session time zone), and the precision is kept. A `TIME WITH TIME ZONE`
    gives the time of the same instant in `zone`, with the offset `zone`
    has at the statement's time, wrapped into a day (`TIMETZ '10:00+02' AT
    TIME ZONE 'America/New_York'` is `04:00:00-04` in July), and a `TIME`
    is first read as a time with time zone in the session time zone, as
    Postgres casts it (`TIME '10:00' AT TIME ZONE 'UTC'` is `14:00:00+00` in
    a New York session in July). `x AT LOCAL` (also `TIMEZONE(x)`) converts
    to or from the session time zone. Up to v0.0.12 a `TIME` was an error
    (`function timezone(varchar, time) does not exist`)
    - `AT TIME ZONE` binds tighter than `*` and `+` and looser than `::`
      and unary minus, left to right, so dbt_date's `cast(cast(x as
      timestamp) at time zone 'UTC' at time zone 'America/Los_Angeles' as
      timestamp)` converts from UTC to Los Angeles time
    - `CONVERT_TIMEZONE(source, target, x)` (Snowflake, Redshift; what
      dbt_date's `convert_timezone` and `now()` emit) reads `x` as a local
      time in `source` and gives the local time in `target`, a
      `TIMESTAMP`. A `TIMESTAMP WITH TIME ZONE` is read as its UTC time, as
      Snowflake reads it. `CONVERT_TIMEZONE(target, x)` reads `x` in UTC and
      also gives a `TIMESTAMP`, as in Redshift (Snowflake gives a
      `TIMESTAMP_TZ`, which has no equivalent here)
    - Zones are looked up in Postgres's order. First the time zone
      abbreviations: the 195 of Postgres's default set
      (`timezone_abbreviations = 'Default'`), in any case. Most are fixed
      offsets: `UTC`, `GMT`, `Z`, `EST` / `EDT`, `PST` / `PDT`, `CET` /
      `CEST`, `MSD`, `EAT`, `JST`, `AEST` / `AEDT`, `NZST` / `NZDT`, …, so
      `CET` is UTC+1 in July too. `IST` is Israel Standard Time, UTC+2, as
      in Postgres; India's zone is `Asia/Kolkata`. The rest are tied to a
      zone, as in Postgres: `MSK` is the offset it had in `Europe/Moscow`
      at that instant (UTC+4 in 2012, UTC+3 now), and one that the tz
      database no longer uses is the zone's offset (`SGT` is
      `Asia/Singapore`'s, which it writes `+08`). Then the IANA zone names
      (`America/New_York`, `Etc/GMT+5`), case-insensitive, from the tz
      database built into the driver. Then POSIX time zones: `UTC+5`,
      `+05:30`, `5` or `<+0530>-05:30`, whose sign is west of Greenwich, as
      in POSIX and Postgres, so `UTC+5` and `+05:30` are behind UTC. A
      POSIX zone can have daylight saving time: `CET-1CEST,M3.5.0,M10.5.0/3`
      is UTC+1, and UTC+2 from the last Sunday of March at 02:00 to the
      last Sunday of October at 03:00. Its rules are as in Postgres: days
      `Mm.w.d`, `Jn` (without February 29) or `n`, times outside a day
      (`M3.2.0/-1`, `M10.4.6/26`), any case, and without rules
      (`XYZ1ABC`) the US rules, Postgres's default. An `INTERVAL` is an
      offset east of Greenwich, as in ISO 8601 (`INTERVAL '+05:30'` is
      ahead of UTC), without months or days (`interval time zone "1 day"
      must not include months or days`). Anything else is `time zone "x"
      not recognized`. Timestamp text with a zone name (`'2024-03-10 02:30
      America/Los_Angeles'`, `'2024-07-01 12:00 MSK'`) reads the zone the
      same way, except a POSIX zone with rules, which Postgres splits at
      its commas. Up to v0.0.12 the driver had 38 of the abbreviations, and
      `WEST` (UTC+1), which isn't one of Postgres's
    - Daylight saving time as in Postgres, in an IANA zone or a POSIX one: a
      local time that the spring-forward transition skips is read with the
      offset before it (`TIMESTAMP '2024-03-10 02:30' AT TIME ZONE
      'America/Los_Angeles'` is 10:30 UTC), and one that the fall-back
      transition repeats with the offset after it (`TIMESTAMP '2024-11-03
      01:30'` is 01:30 PST, 09:30 UTC)
- Session time zone (`SET TIME ZONE`, `SET timezone = …`, the
  `adbc.redis.time_zone` option; UTC by default). Each connection has its
  own. As in Postgres, a `TIMESTAMP WITH TIME ZONE` is an instant, and the
  session time zone is where it is shown and read as a local time:
  - **Text:** casts to text, `||`, `CONCAT`, `STRING_AGG`, `LIKE` and the
    other functions that read text, `TO_CHAR` (`TZ`, `OF`, `TZH`, `TZM`)
    and JSON write its local time with the zone's offset at that instant:
    `2024-01-09 21:00:00-08` and `2024-07-01 11:45:30.25-07` in
    `America/Los_Angeles`, `2024-01-10 10:30:00+05:30` in `Asia/Kolkata`.
  - **Input:** text without an offset or zone (`'2024-01-10 02:00'`,
    `TIMESTAMPTZ '2024-01-10 02:00'`, a bound string parameter, a value
    written to a column, also by bulk ingest) is a local time; text with an
    offset or a zone name keeps it. A time with time zone without an offset
    (`'10:00'`, `TIMETZ '10:00'`), and a `TIME` converted to one, take the
    zone's offset on the current date, as in Postgres: `10:00:00-04` in New
    York in July, `10:00:00-05` in January.
  - **Conversions:** a `TIMESTAMP` becomes a `TIMESTAMP WITH TIME ZONE` as a
    local time, and back as its local time; a `DATE` is local midnight, and
    a timestamp with time zone's `DATE` and `TIME` are its local date and
    time. These also apply where Postgres converts implicitly: comparisons
    (`tz_col = TIMESTAMP '…'`, `tz_col >= current_date`), `IN`, `BETWEEN`,
    `GREATEST` / `LEAST`, `COALESCE`, `CASE`, `UNION`, joins, and writes to a
    column of the other type. `x AT LOCAL` converts to or from the session
    time zone.
  - **Bulk ingest** converts as `INSERT` does: an Arrow string without an
    offset, an Arrow timestamp without a zone and an Arrow date written to a
    `TIMESTAMP WITH TIME ZONE` column are local times, and an Arrow
    timestamp with a zone written to a `TIMESTAMP`, `DATE`, `TIME` or text
    column is its local time. Arrow timestamps with a zone, and text with an
    offset, keep their instant. Up to v0.0.11, bulk ingest converted in UTC.
  - **Fields and arithmetic:** `EXTRACT` / `DATE_PART` and the field
    functions, `DATE_TRUNC` (a day or longer starts at local midnight; a
    shorter unit keeps the offset, so `01:30 PST` truncates to `01:00 PST`
    on a fall-back day), `AGE`, `DATEDIFF` / `DATE_DIFF`, `LAST_DAY` and
    `TO_CHAR` read the local time. Adding an interval adds its months and
    days to the local time and its time as elapsed time, so across the
    spring-forward transition `+ INTERVAL '1 day'` is 23 hours and `+
    INTERVAL '24 hours'` 24 (also `DATEADD`, `GENERATE_SERIES` and window
    `RANGE` offsets). `DATE_BIN`'s stride is elapsed time; only its origin
    is read as a local time.
  - **The current time:** `CURRENT_DATE`, `LOCALTIMESTAMP`, `CURRENT_TIME`
    and `LOCALTIME` are local (`CURRENT_TIME` with the zone's offset);
    `NOW()` is the instant. `MAKE_TIMESTAMPTZ` and `TO_TIMESTAMP(text,
    format)` read a local time, unless the format has `TZH` or `TZM`.
  - **Daylight saving time,** as in Postgres: a local time that the
    spring-forward transition skips is read with the offset before it
    (`'2024-03-10 02:30'` in Los Angeles is `2024-03-10 03:30:00-07`), and
    one that the fall-back transition repeats with the offset after it
    (`'2024-11-03 01:30'` is `01:30:00-08`).
  - **What it doesn't change:** stored values (instants), Arrow results (a
    `TIMESTAMP WITH TIME ZONE` column is an Arrow timestamp with time zone
    `UTC`, with the same values in every session time zone), and
    `CONVERT_TIMEZONE`, which reads a timestamp with time zone as its UTC
    time (as Snowflake does).
  - **Index queries** work on UTC instants: a local-time constant is
    converted in the session time zone before it is pushed down (see "How
    SQL is executed"), so the index returns the rows the comparison does.
  - **Views, defaults and `CHECK`s** read a local-time literal once, in the
    session time zone of the statement that defines them (`CREATE VIEW`,
    `CREATE TABLE`, `ALTER TABLE`), as Postgres does. The literal is stored
    as its instant, with that zone's offset: `'2024-01-01 00:00'` in New
    York is stored as `'2024-01-01 00:00:00-05'`, which `information_schema`
    shows (Postgres's `pg_get_viewdef` shows `'2024-01-01
    00:00:00-05'::timestamp with time zone`). So a view created in New York
    returns the same rows in every session, and a `CHECK (ts >=
    '2024-01-01')` defined there rejects the same rows in every session. A
    local-time literal is text without an offset that is cast to a
    timestamp with time zone or a time with time zone (`TIMESTAMPTZ '…'`,
    `'…'::timestamptz`, `TIMETZ '…'`, `'…'::timetz`) or read as one:
    compared with one (also in `IN`, `BETWEEN` and a `CASE` operand), a
    result of a `CASE` or `IIF` or an argument of `COALESCE`, `GREATEST`,
    `LEAST` or `NULLIF` with one, the operand of `AT TIME ZONE` and `AT
    LOCAL`, and the `DEFAULT` of such a column. A time with time zone is stored with the offset the zone has
    that day (`'10:00'` in New York in July is `'10:00:00-04'`), as
    Postgres stores `'10:00:00-04'::time with time zone`. The conversions
    the definition makes when it runs read the zone of the session that
    runs it, as in Postgres:
    casts of columns and of other values (`ts::date`, `DATE
    '2024-01-01'::timestamptz`, a `TIMESTAMP` compared with a timestamp
    with time zone), `CURRENT_DATE`, `LOCALTIMESTAMP` and text output.
    - **Upgrading from v0.0.12 and earlier:** they read such a literal in
      the zone of the session that queried the view or wrote the row.
      Views, defaults and `CHECK`s they stored keep doing so; define them
      again (`CREATE OR REPLACE VIEW`, `ALTER TABLE … ALTER COLUMN … SET
      DEFAULT`, `DROP CONSTRAINT` and `ADD CONSTRAINT`) to store the
      instant.
  - **Values:** the forms in the `TimeZone` row of the parameter table
    under "Transactions and settings". An IANA name is looked up
    case-insensitively and shown as the tz database spells it
    (`america/new_york` is `America/New_York`), and a POSIX time zone in
    upper case (`CET-1CEST,M3.5.0,M10.5.0/3`, with daylight saving time).
    Here, as in Postgres, abbreviations aren't read, so `SET TIME ZONE
    'CET'` is the zone, with summer time (while `AT TIME ZONE 'CET'` is the
    abbreviation, UTC+1). Numbers and `INTERVAL`s are east of Greenwich, as
    in Postgres's `SET TIME ZONE` (`-8` and `INTERVAL '-08:00' HOUR TO
    MINUTE` are 8 hours behind UTC), while a POSIX time zone is west of it
    (`UTC+5` is 5 hours behind). An unknown zone, and an abbreviation that
    isn't a zone name (`PST`, `IST`), are `invalid value for parameter
    "TimeZone": "x"`, as in Postgres. Up to v0.0.12 the driver read such an
    abbreviation as a fixed offset.
  - **Differences from Postgres:** `MAKE_TIMESTAMPTZ` with a zone argument
    isn't supported.
- Intervals and date/time arithmetic:
  - `INTERVAL '1 year 2 months 3 days 04:05:06'`, `'1.5 hours'`, `'2 days ago'`,
    `'3 04:05:06'`, `'1-2'` (years-months), ISO 8601 `'P1Y2M3DT4H'` (also
    in the alternative format, `'P0001-02-03T04:05:06'`),
    `INTERVAL '2' HOUR`, `INTERVAL '1-2' YEAR TO MONTH`, `INTERVAL '1.5'
    SECOND(0)`, `INTERVAL(0) '1.5'`, `INTERVAL 7 DAY` / `INTERVAL ? DAY`,
    and `CAST('7 days' AS INTERVAL)`
    - A literal is read as Postgres 16 reads it. Each field's integer part
      is exact, and its fraction cascades into the smaller units: a month
      is 30 days, a week 7 days and a day 24 hours (`'1.5 months'` is `1
      mon 15 days`, `'0.1 mon'` is `3 days`), and a fraction of a year,
      decade, century or millennium is whole months, rounded half to even
      (`'0.125 years'` is `2 mons`). The time is rounded to microseconds,
      as Postgres's always is, though an interval holds nanoseconds: the
      fraction of a unit rounds half a microsecond toward zero (`'0.0000015
      seconds'` is `00:00:00.000001`, `'1.0000005 sec'` is `00:00:01`), and
      a clock's fraction of a second rounds half to even
      (`'00:00:00.0000015'` is `00:00:00.000002`). So does `n` in `INTERVAL
      n UNIT`. Bound and ingested Arrow `month_day_nano_interval` values
      keep their nanoseconds
    - A clock with a fraction and two fields is minutes and seconds
      (`'1:30.5'` is `00:01:30.5`). Its minutes go up to 59 and its seconds
      to 60, and the months of years-months to 11: past them, it is
      `interval field value out of range: "1:75:00"`, as in Postgres
    - The fields are read from right to left, as in Postgres, so that a
      unit comes before its number: a number without a unit has the unit
      of the number to its right, but after hours or a clock it is days
      (`'1 2 hours'` is `1 day 02:00:00`, `'1-2 years'` is `1 year 2
      mons`), and the rightmost is seconds (`'1 day 5'` is `1 day
      00:00:05`). Each unit is given once: `'1 day 2 days'`, `'01:00
      02:00'` and `'1.5 seconds 3 microseconds'` (a fraction of a second is
      its milliseconds and microseconds too) are `invalid input syntax for
      type interval: "…"`, as is `'1 2'`. `ago` may be anywhere (`'1 day
      ago 2 hours'` is `-1 days -02:00:00`). The units are Postgres's, which
      include `c`, `cent`, `dec`, `mil`, `usecond` and `msecond`, and
      punctuation between fields is a separator (`'@ 1 day, 2 hours'`).
      An ISO 8601 number is read as C's `strtod` reads it, also in
      hexadecimal (`'P0x10Y'` is `16 years`)
    - With fields (`INTERVAL '…' HOUR`, `DAY TO MINUTE`, `MINUTE TO SECOND`
      and Postgres's other qualifiers), the rightmost number without a
      unit is the last field, and the fields after it are dropped, the
      time truncated toward zero, as in Postgres: `INTERVAL '1.5' HOUR` is
      `01:00:00`, `INTERVAL '1 day 02:30' HOUR` is `1 day 02:00:00`, and
      `INTERVAL '1:30' MINUTE TO SECOND` is `00:01:30`. `SECOND(p)` and
      `INTERVAL(p) '…'` round the time to `p` decimals, half away from
      zero. `INTERVAL n HOUR` and `INTERVAL ? HOUR` drop them too
      (`INTERVAL 1.5 HOUR` is `01:00:00`)
    - **Differences from Postgres:** `INTERVAL n UNIT` with an expression,
      and `INTERVAL 'n' UNIT` with a unit that isn't a qualifier's field
      (`HOURS`, `WEEK`, `QUARTER`), which keeps its fraction (`INTERVAL
      '1.5' HOURS` is `01:30:00`), are driver extensions; Postgres reads
      `INTERVAL '1.5' hours` as an interval of 1.5 seconds named `hours`.
      The qualifier of a column or a cast (`CAST(x AS INTERVAL HOUR)`) is
      ignored
    - **Upgrading from v0.0.12 and earlier:** the fields were read from
      left to right and added up: `'1 2 hours'` was `02:00:01`, `'1 2'`
      `00:00:03`, `'1-2 years'` an error, and a unit given twice was
      summed (`'1 day 2 days'` was `3 days`). The driver also read
      quarters (`'1 quarter'` was `3 mons`), lower-case ISO 8601
      (`'p1d'`), ISO 8601 text with spaces around it (`CAST(' P1D' AS
      INTERVAL)`), `'1.seconds'`, `'-.5 hours'`, `'ago'` (`00:00:00`) and
      `'1 ago'`, and `ago` only at the end, but not `'1 c'`, `'1d2h'` or
      `'@ 1 day'`. `INTERVAL '1.5' HOUR` and `INTERVAL 1.5 HOUR` were
      `01:30:00`, `INTERVAL '1 day 02:30' HOUR` was "INTERVAL HOUR needs a
      number", `INTERVAL '1e3' SECOND` was `00:16:40`, and any units were
      accepted around `TO` (`YEAR TO DAY`). Text that isn't an interval
      was `invalid interval "…"` (with the same SQLSTATE, 22007), and a
      field past its part of the interval (`'2147483648 days'`) was
      "interval out of range" (22008), not `interval field value out of
      range: "…"` (22015)
    - **Upgrading from v0.0.12 and earlier:** the fields were summed as
      nanoseconds in doubles, so a literal's time past about 2,500 hours
      wasn't exact (`'1000000:00:00.000001'` was `1000000:00:00.000001024`),
      and digits past microseconds were kept (`'0.0000015 seconds'` was
      `00:00:00.0000015`). A fraction of a year cascaded into days
      (`'0.05 years'` was `18 days`; it is `1 mon`), `'1:30.5'` was 1 hour
      30.5 minutes, `'1:75:00'` and `'1-12'` were accepted, and the ISO 8601
      alternative format was an error. No literal is exactly -2^63
      nanoseconds any more: `'-9223372036.854775808 seconds'` rounds to
      `-2562047:47:16.854776` (Postgres's value), past the nanoseconds'
      range, so it is "interval out of range"
  - `timestamp ± interval` (a timestamp with time zone adds months and
    days in the session time zone), `date ± interval` (gives a timestamp),
    `date ± integer` (days), `date - date` (days), `timestamp - timestamp`
    (an interval of days and time), `time ± interval`, `time - time`,
    `timetz ± interval` (the interval's time part, wrapped around midnight;
    the offset is kept), `date + timetz` (a timestamp with time zone: the
    time at its offset on that date), `interval ± interval`, `interval *
    n`, `interval / n`, `-interval`
  - Months follow the calendar and clamp to the end of the month
    (`TIMESTAMP '2024-01-31' + INTERVAL '1 month'` is Feb 29). Comparisons
    treat a month as 30 days and a day as 24 hours, so `INTERVAL '1 mon' =
    INTERVAL '30 days'` and `INTERVAL '1 day' = INTERVAL '24 hours'`, and so
    do `ORDER BY`, `GROUP BY`, `DISTINCT`, joins and set operations, as in
    Postgres (see `GROUP BY`)
  - `interval * n` and `interval / n` work as in Postgres, with `n` as a
    `DOUBLE PRECISION`: each field is multiplied (or divided) and
    truncated, the fraction of a month becomes days, 30 to a month,
    rounded to millionths of a day, the fraction of a day becomes time,
    and the time is rounded to microseconds, half to even. So `INTERVAL '1
    sec' / 3` is `00:00:00.333333`, `INTERVAL '1 mon' / 7` is `4 days
    06:51:25.6896` (30 / 7 is 4.285714 days), and `INTERVAL '1 mon 3 days'
    / 4` is `8 days 06:00:00`. The time never carries into days: `INTERVAL
    '29 days 23:59:59.999999' * 0.5` is `14 days 24:00:00`. Months or days
    past 32 bits (also once a cascaded fraction adds a day), a NaN and an
    infinite `n` are "interval out of range" (dividing by an infinity is
    0), and `/ 0` is "division by zero"
    - An interval's time is nanoseconds here and microseconds in
      Postgres. A time that isn't whole microseconds (a bound or ingested
      Arrow `month_day_nano_interval`, or the difference of two
      `TIMESTAMP(9)`s or `TIME(9)`s) counts with its fraction, and the
      result is whole microseconds, so even `* 1` rounds it: 2.5 µs × 1 is
      `00:00:00.000002`. The result must fit the nanoseconds' range, about
      ±2,562,047 hours, 1000 times shorter than Postgres's
    - The doubles are computed as Postgres does on x86-64. The arm64
      build in the `postgres:16` Docker image fuses multiply-adds (its
      compiler's default), so there `interval * n` and `PERCENTILE_CONT`
      can differ in the last microseconds when the fields are large:
      `INTERVAL '2147483647 mons' * 0.3` is `53687091 years 2 mons 3 days
      00:00:00.0864` on x86-64 and `53687091 years 2 mons 2 days
      23:59:59.9136` on arm64
    - **Upgrading from v0.0.12 and earlier:** the fractions cascaded in
      nanoseconds and weren't rounded: `INTERVAL '1 sec' / 3` was
      `00:00:00.333333333` and `INTERVAL '1 mon' / 7` `4 days
      06:51:25.714285714`. `PERCENTILE_CONT` and `MEDIAN` of intervals
      changed with them (see Aggregates)
  - A result outside its type's range is an error, as in Postgres, rather
    than wrapping. A `DATE` is an Arrow `date32` (5877642-06-23 BC, year
    -5877641, to 5881580-07-11): past it, `date ± integer`, `MAKE_DATE`,
    `DATEADD`, `TO_DATE`, the other date functions and casts to `DATE` give
    "date out of range". A
    timestamp past the range of its unit (microseconds: about ±292,000 years
    from 1970) is "timestamp out of range". An interval's time part is
    nanoseconds in 64 bits (about ±2,562,047 hours); past that, and for a
    months or days count beyond 32 bits, it is "interval out of range"
  - `AGE(a, b)` / `AGE(x)` (years, months, days and time), and `EXTRACT`
    and `TO_CHAR` on intervals
  - `SUM`, `AVG`, `MIN` and `MAX` of intervals, also as window functions,
    are intervals (see Aggregates)
  - Intervals are returned as Arrow `month_day_nano_interval`; Arrow
    interval and duration values can be bound and ingested
- JSON stored as text in `VARCHAR` columns (there is no JSON column type).
  Documents are parsed by the driver in each call, on the rows the index
  returns; nothing JSON-specific is pushed down. Semantics follow Postgres:
  - Operators: `doc -> 'key'` and `doc -> n` (an array element; negative `n`
    counts from the end) return JSON, `doc ->> …` returns text (a string's
    value, NULL for JSON null); `doc #> '{a,0,b}'` and `#>>` follow a path
    given as a text array. As in Postgres they share one precedence level
    with `||`, left to right, below `+` / `-` and above comparisons:
    `doc ->> 'a' || '!'` is `(doc ->> 'a') || '!'`, while `'x' || doc ->> 'a'`
    applies `->>` to the concatenation
  - `JSON_EXTRACT_PATH(doc, key, …)`, `JSON_EXTRACT_PATH_TEXT`,
    `JSON_TYPEOF`, `JSON_ARRAY_LENGTH`; `x::json` / `CAST(x AS JSON)` checks
    the text, `x::jsonb` normalizes it (both give `VARCHAR`)
  - These read the json type: a value keeps its text from the document, so
    numbers are never rounded (`'{"n": 1e400}' ->> 'n'` is `1e400`) and of
    duplicate keys the last one wins. Malformed JSON is an error,
    `invalid input syntax for type json: …` with Postgres's detail, and so
    are `\u` escapes Postgres can't decode (code point 0, unpaired
    surrogates). `doc IS JSON` guards against bad rows
  - The `JSONB_` spellings (`JSONB_EXTRACT_PATH`, `JSONB_TYPEOF`,
    `JSONB_BUILD_OBJECT`, `JSONB_AGG`, …), `TO_JSONB`, `::jsonb` and
    `RETURNING JSONB` give jsonb's text: keys sorted (shorter first) without
    duplicates, numbers as exact `NUMERIC` (`1e2` is `100`, `1.50` stays
    `1.50`), and `{"a": 1}` spacing. Up to v0.0.12 `TO_JSONB` gave JSON's
    text
  - `x IS [NOT] JSON [VALUE | SCALAR | ARRAY | OBJECT]
    [{WITH | WITHOUT} UNIQUE [KEYS]]` is false for malformed text and NULL
    for NULL
  - SQL/JSON query functions, as in Postgres 17 (documents are read as
    jsonb): `JSON_VALUE(doc, path [RETURNING type] [behavior ON EMPTY]
    [behavior ON ERROR])` with `ERROR`, `NULL` or `DEFAULT expr`;
    `JSON_QUERY(doc, path [RETURNING type]
    [{WITHOUT | WITH [CONDITIONAL | UNCONDITIONAL]} [ARRAY] WRAPPER]
    [{KEEP | OMIT} QUOTES] …)`, which also allows `EMPTY [ARRAY]` and
    `EMPTY OBJECT`; `JSON_EXISTS(doc, path [{TRUE | FALSE | UNKNOWN | ERROR}
    ON ERROR])`. The defaults are `NULL ON EMPTY` and `NULL ON ERROR`
    (`FALSE ON ERROR` for `JSON_EXISTS`). `ON ERROR` covers a malformed
    document, a strict-mode path error, a result of the wrong shape (several
    items, or an object for `JSON_VALUE`) and a failed conversion to the
    `RETURNING` type; an invalid path is always an error
  - Paths: `[lax | strict] $` followed by `.key`, `."key"`, `.*`, `[n]`,
    `[last]`, `[last - n]`, `[a to b]`, lists like `[0, 2 to last]`, and
    `[*]`. Lax mode (the default) applies `.key` to each element of an
    array, treats a non-array as an array of one for `[n]`, and skips
    missing keys and out-of-range subscripts; strict mode makes them errors.
    Filters (`?(…)`), item methods (`.size()`, …), arithmetic, `.**` and
    `PASSING` variables are not supported
  - Constructors: `JSON_BUILD_OBJECT(k, v, …)`, `JSON_BUILD_ARRAY(…)`,
    `JSON_OBJECT(k VALUE v | k : v, … [{NULL | ABSENT} ON NULL]
    [{WITH | WITHOUT} UNIQUE [KEYS]] [RETURNING JSON | JSONB | VARCHAR])`
    (and Postgres's `JSON_OBJECT('{k,v,…}')` / `JSON_OBJECT(keys, values)`
    over text arrays), `JSON_ARRAY(v, … [{ABSENT | NULL} ON NULL]
    [RETURNING …])`, `TO_JSON(x)`. Values are encoded as Postgres's
    `to_json` does: numbers and booleans as such, NULL as `null`, strings
    escaped, dates and timestamps as ISO 8601 strings
    (`"2024-01-15T10:30:00+00:00"`), binary values as `"\\x…"`, and with
    Postgres's spacing (`{"a" : 1}`, `[1, 2]`)
  - Aggregates, computed by the driver: `JSON_AGG(x [ORDER BY …])` and
    `JSON_OBJECT_AGG(k, v [ORDER BY …])`, also with `DISTINCT` (whose values
    come out sorted). NULL values are `null`, a NULL key is an error, and no
    rows give NULL. `ORDER BY` puts NULLs last by default in either
    direction, like the driver's `ORDER BY` (Postgres puts them first for
    `DESC`). They are not available as window functions
  - Results are `VARCHAR`. An argument is embedded as JSON when it is itself
    JSON: a JSON function, `->`, `#>` or `::json`. Any other text becomes a
    JSON string, including JSON read back from a table, view or subquery, so
    write `col::json` to embed it
  - Differences from Postgres: doubles keep the driver's text form (`1e+06`
    where Postgres writes `1000000`), and the SQL/JSON functions apply
    `ON ERROR` to a malformed document too (Postgres raises the error when
    it converts the text to jsonb)
  - Not supported: the other jsonb operators (`@>`, `?`, `||` on jsonb,
    `-`), set-returning functions (`JSON_EACH`, `JSON_ARRAY_ELEMENTS`,
    `JSON_TABLE`), `JSONB_SET` and the other modification functions,
    `JSON_ARRAY(SELECT …)`, and a JSON column type (a RedisJSON-backed one
    is possible future work)

### Additional statements and metadata

- `SELECT` without `FROM` for literal expressions
- `information_schema` (read-only, built from the driver's metadata when
  queried): `schemata`, `tables` (`BASE TABLE` / `VIEW` / `LOCAL TEMPORARY`,
  each table's row `key_prefix` and `index_name`, NULL for views, and
  `comment`), `columns` (`ordinal_position`, `column_default`, `data_type`, `is_nullable`,
  `character_maximum_length`, `character_octet_length`, `numeric_precision`,
  `numeric_scale`, `datetime_precision`, `is_indexed` and `comment`), `views`
  (`view_definition`), `table_constraints` and `key_column_usage` (the
  primary keys and `CHECK` constraints, see Constraints above), and
  `routines`, the driver's functions (see "Function calls" above). `comment` is the `COMMENT ON` text, NULL without
  one. `data_type` is the driver's name of the type, with its length or
  precision (`VARCHAR(3)`, `TIMESTAMP(2)`, `TIMESTAMP(6) WITH TIME ZONE`),
  where Postgres writes `character varying` or `timestamp without time
  zone` and keeps the number in `character_maximum_length` or
  `datetime_precision`, which have it here too. Any SQL works on them,
  including joins
- `[WITH …] UPDATE t [[AS] a] SET col = …, … [FROM item, …] [WHERE …]` and
  `[WITH …] DELETE FROM t [[AS] a] [USING item, …] [WHERE …]` (the Postgres
  forms). The FROM / USING items are written like a SELECT's FROM clause
  (tables, views, CTEs and `(SELECT …)`, with commas or joins) and are
  joined with the target; SET and WHERE can read all of them. A target row
  that matches several FROM / USING rows is updated or deleted once. For
  UPDATE the matches must give the same new values, otherwise it's an error
  (Postgres silently uses one of them)
- `UPDATE … SET (a, b) = …` assigns a row to a column list, as in Postgres,
  also in `UPDATE … FROM` and in MERGE's `UPDATE SET`. The row is
  `(x, y, …)`, `ROW(x, …)` (also for a single column) or a subquery
  `(SELECT x, y …)`, which must return as many columns ("number of columns
  does not match number of values") and at most one row: no row sets the
  columns to NULL, more than one is an error. The subquery runs once per
  row (once in all if it isn't correlated), not once per column. As with
  `col = …`, every value is computed from the row before the change, so
  `SET (a, b) = (b, a)` swaps
- `[WITH …] MERGE INTO t [[AS] a] USING item [[AS] s] ON cond` followed by
  any number of
  - `WHEN MATCHED [AND cond] THEN UPDATE SET … | DELETE | DO NOTHING`
  - `WHEN NOT MATCHED [BY TARGET] [AND cond] THEN INSERT [(cols)] VALUES (…)
    | INSERT DEFAULT VALUES | DO NOTHING`
  - `WHEN NOT MATCHED BY SOURCE [AND cond] THEN UPDATE SET … | DELETE | DO NOTHING`

  The source is a table, view, CTE or `(SELECT …)`. Each source row with
  its matching target row, each unmatched source row and (with `BY SOURCE`
  clauses) each unmatched target row takes the first clause of its kind
  whose condition holds, as in Postgres. `WHEN NOT MATCHED` clauses see only
  the source's columns (so `VALUES (id, name)` reads the source), `BY
  SOURCE` clauses only the target's. Changing a target row for more than one
  source row is an error ("MERGE command cannot affect row a second time");
  matching more than one is fine if only one match changes it. NULL keys
  never match. The result is the number of rows inserted, updated and
  deleted. `INSERT` gives the columns it leaves out (or sets to `DEFAULT`)
  their defaults, and `INSERT DEFAULT VALUES` inserts a row of defaults
- `RETURNING …` at the end of `INSERT`, `UPDATE`, `DELETE` and `MERGE`,
  written like a select list: `*`, `t.*`, and expressions with optional
  aliases, including subqueries (aggregates and window functions are not
  allowed, as in Postgres). It reads the target's columns, and in
  `UPDATE … FROM`, `DELETE … USING` and `MERGE` the other items' too. As in
  Postgres, `*` lists the target's columns first, then the FROM / USING
  items'; in `MERGE`, the source's columns, then the target's. The
  statement returns one row per row it changed (`INSERT`: in `VALUES` or
  query order; otherwise in no particular order):
  - `INSERT`: the new row as stored (cast to the column types);
    `RETURNING __rowid` gives the row id it was given
  - `UPDATE`: the row after the change
  - `DELETE`: the deleted row
  - `MERGE`: each row inserted, updated or deleted, with the source row it
    came from (NULLs for `WHEN NOT MATCHED BY SOURCE`); `merge_action()`
    (Postgres 17) is `'INSERT'`, `'UPDATE'` or `'DELETE'`

  The list is computed with the other checks, before anything is written, so
  an error in it leaves the table untouched, and its subqueries see the
  tables as they were before the statement. `ExecuteQuery` returns the rows
  (with the RETURNING columns, also when no row changed); `ExecuteUpdate`
  runs the statement and returns the number of rows changed, as Postgres
  drivers do; `ExecuteSchema` returns the columns without running it. Without
  `RETURNING`, `ExecuteQuery` on these statements still returns an empty
  result with no columns, and the number of rows changed. `__rowid` can't be
  read in the `RETURNING` list of `UPDATE … FROM`, `DELETE … USING` or
  `MERGE` (as in any join), and `merge_action()` can't be called inside a
  subquery

### Transactions, limitations, and types

- Transactions and settings. The driver is autocommit only: every statement
  commits on its own. SQL written for Postgres still sends transaction
  control and `SET` (dbt sends a literal `commit;` before hooks that run
  outside the transaction, and `sql_header` often holds `SET`s), so they are
  accepted as Postgres accepts them in autocommit mode:
  - **Transaction control does nothing:** `BEGIN [WORK | TRANSACTION]
    [mode, …]`, `START TRANSACTION [mode, …]`, `COMMIT [WORK | TRANSACTION]
    [AND [NO] CHAIN]`, `END …`, `ROLLBACK [WORK | TRANSACTION] [AND [NO]
    CHAIN]` and `ABORT …`, and also `SET TRANSACTION mode, …` and `SET
    SESSION CHARACTERISTICS AS TRANSACTION mode, …`. The modes
    (`ISOLATION LEVEL …`, `READ ONLY`, `READ WRITE`, `[NOT] DEFERRABLE`) are
    accepted and ignored, so `READ ONLY` doesn't stop writes. **`ROLLBACK`
    undoes nothing**: in `BEGIN; INSERT …; ROLLBACK` the row stays, because
    the `INSERT` committed. Postgres warns when there is no transaction to
    commit or roll back; ADBC has no way to return warnings, so nothing is
    reported. `COMMIT AND CHAIN` outside `BEGIN` is Postgres's error
    `COMMIT AND CHAIN can only be used in transaction blocks`.
  - **Not supported:** `SAVEPOINT`, `RELEASE [SAVEPOINT]`, `ROLLBACK TO
    [SAVEPOINT]`, `COMMIT PREPARED` / `ROLLBACK PREPARED` and `SET
    TRANSACTION SNAPSHOT` (`SAVEPOINT is not supported (autocommit only)`, …).
    The error comes when the script is parsed, so none of its statements
    run.
  - **`SET [SESSION | LOCAL] name {= | TO} {value [, …] | DEFAULT}`,
    `RESET name`, `RESET ALL`, `SHOW name` and `SHOW ALL`**, also `SET TIME
    ZONE …`, `RESET TIME ZONE`, `SHOW TIME ZONE`, `SET SCHEMA 'name'`
    (`search_path`) and `SET NAMES 'name'` (`client_encoding`), for the
    parameters below. Names are case-insensitive. Other names, Postgres's
    custom `a.b` ones included, give `unrecognized configuration parameter
    "x"`. `SHOW` returns one `VARCHAR` column named as in Postgres
    (`DateStyle`, `TimeZone`, …); `SHOW ALL` returns `name`, `setting` and
    `description`. Settings belong to the connection and last until it
    closes; `ROLLBACK` doesn't undo them either.
  - **`SET LOCAL`** lasts until the transaction ends, as in Postgres: until
    `COMMIT`, `END`, `ROLLBACK` or `ABORT` after a `BEGIN` (the connection
    remembers that a `BEGIN` is open, for this only), or else until the end
    of the script it is in (Postgres runs a script of several statements as
    one transaction). A `SET LOCAL` statement on its own, outside `BEGIN`,
    is checked and has no effect, as in Postgres.
  - **Parameters:**

    | Parameter | Values | Default |
    |-|-|-|
    | `search_path` | Schemas, comma-separated. Unqualified names use the first one, skipping `"$user"`, `pg_catalog` and `pg_temp` (temporary objects are always found first), as `adbc.redis.default_schema` sets it: `CREATE`, queries, writes and bulk ingest. The schema needn't exist yet (schemas exist once they have a table). `SHOW` shows the list as set, and the ADBC current schema (`adbc.connection.db_schema`) is the same setting | `adbc.redis.default_schema` |
    | `TimeZone` (`SET TIME ZONE`) | The session time zone (see Session time zone): an IANA zone name (case-insensitive, shown as the tz database spells it), a POSIX time zone (`UTC+5`, west of Greenwich, or with daylight saving time, `CET-1CEST,M3.5.0,M10.5.0/3`; shown in upper case), a number of hours east of Greenwich (`-8`, shown `<-08>+08`), or `INTERVAL '+05:30' [HOUR TO MINUTE]` (east, shown `<+05:30>-05:30`). `LOCAL` and `DEFAULT` are the default. Others give `invalid value for parameter "TimeZone": "…"`, also a time zone abbreviation (`PST`), as in Postgres | `adbc.redis.time_zone`, or `UTC` |
    | `client_encoding` | Only `UTF8` (`UTF-8`, `UNICODE`) | `UTF8` |
    | `application_name` | Any; not used | `''` |
    | `standard_conforming_strings` | Only `on`: `'…'` strings don't treat backslashes as escapes | `on` |
    | `statement_timeout`, `lock_timeout`, `idle_in_transaction_session_timeout` | Milliseconds, or a number with a unit (`us`, `ms`, `s`, `min`, `h`, `d`), shown as Postgres shows them (`5s`, `1500ms`). Not enforced: there is no statement timeout (and there are no locks or transactions) | `0` |
    | `extra_float_digits` | -15 to 3; no effect, as results are Arrow values, not text | `1` |
    | `DateStyle` | Only the `ISO` style, with an order `MDY`, `DMY` or `YMD`, which has no effect (only ISO dates are read) | `ISO, MDY` |
    | `IntervalStyle` | Only `postgres`, the format of intervals cast to text | `postgres` |

    The same settings through ADBC options are not supported, other than
    the current schema and the time zone (`adbc.redis.time_zone`).
- Not supported: mutually recursive CTEs, `ANY` / `ALL` over arrays or
  value lists, set-returning functions in the SELECT list
  (`SELECT generate_series(1, 3)`), table functions other than
  `GENERATE_SERIES`, data-modifying statements in `WITH` (`WITH d AS
  (DELETE … RETURNING …) INSERT …`), `RETURNING OLD.* / NEW.*` (Postgres
  18), `GROUP BY DISTINCT` (which drops repeated grouping sets),
  `GROUPING()` of an enclosing query's columns inside a subquery,
  `PERCENTILE_CONT` / `PERCENTILE_DISC` of an array of fractions (there is
  no array type), and the ordered-set aggregates and `MEDIAN` as window
  functions
- Types: `BOOLEAN, SMALLINT, INTEGER, BIGINT, REAL, DOUBLE PRECISION,
  NUMERIC(p,s), VARCHAR(n), CHAR(n), VARCHAR/TEXT, VARBINARY/BLOB, DATE,
  TIME(p) [WITH TIME ZONE], TIMESTAMP(p) [WITH TIME ZONE], INTERVAL`
  (interval and time with time zone columns are stored but not indexed; see
  String lengths for `VARCHAR(n)` / `CHAR(n)`). A `TIMESTAMP WITH TIME ZONE`
  is a UTC instant (an Arrow timestamp with time zone `UTC`), shown and read
  as a local time in the session time zone.
  JSON documents are stored in `VARCHAR` columns (`NOINDEX` if they are
  never compared as a whole); `JSON` and `JSONB` are only cast targets.
  A row value has type `RECORD`, which no column can have, and is returned
  as an Arrow struct (see Rows as values)
  - **Time with time zone:** `TIME(p) WITH TIME ZONE` (also `TIMETZ(p)`) is
    Postgres's: a time of day and a UTC offset, kept as written, with no
    date. `p` is 0 to 6 (6 without it; a `p` above 6 is 6, as in Postgres),
    and a value is rounded to it as a `TIME(p)` is.
    - Text in: a time and an offset (`'10:00+02'`, `'10:00:00.5-05:30'`,
      `'10:00 +0530'`, `'10:00Z'`, up to `±15:59:59`) or an abbreviation
      (`EST`, `CEST`, `MSK`). A zone name needs a date before the time,
      whose offset it takes (`'2024-07-01 10:00 America/New_York'` is
      `10:00:00-04`), unless the zone has only ever had one offset
      (`Etc/GMT+5`); `'10:00 America/New_York'` is `invalid input syntax
      for type time with time zone`, as in Postgres. Without an offset, the
      session time zone's on the current date (see Session time zone).
      Text out: `10:00:00+02`, `10:00:00.5-05:30`
    - Comparisons, as Postgres's: by the time in UTC, then larger offsets
      first. So `'10:00+02' = '09:00+01'` is false and `'10:00+02' <
      '09:00+01'`; `'23:00-05'` (04:00 UTC the next day) sorts after
      `'24:00+00'`. `ORDER BY`, `GROUP BY`, `DISTINCT`, joins, `IN`, `MIN` /
      `MAX` and window `RANGE` frames (the time ± the interval, with the
      row's offset) compare that way. A `TIME` compared with one is read as
      one in the session time zone
    - Conversions: a `TIME` takes the session time zone's offset on the
      current date, a `TIMESTAMP WITH TIME ZONE` becomes its local time and
      offset in the session time zone, and a time with time zone becomes a
      `TIME` by dropping its offset (also when written to a `TIME` column).
      A `TIMESTAMP`, a `DATE` and other types don't convert, as in
      Postgres. `CASE`, `COALESCE`, `GREATEST`, `LEAST` and `UNION` of a
      `TIME` and a time with time zone give a time with time zone
    - Arrow has no such type, so a time with time zone is a string column
      (`utf8`) of its text, whose field metadata
      `ARROW:FLIGHT:SQL:TYPE_NAME` (Flight SQL's) names its type, `TIME(6)
      WITH TIME ZONE`, in query results and `GetTableSchema`. Postgres's
      own ADBC driver has no mapping either: it returns Postgres's binary
      form as an opaque binary column. Bound strings and ingested strings
      are read as text in; bulk ingest that creates a table makes a string
      field with that metadata a time with time zone column, so a result
      ingests back as the same type
    - Rows store the text (`10:00:00+02`). The index doesn't hold such a
      column, so comparisons with it are checked by the driver on the rows
      the rest of the query finds, and it is sorted and grouped by the
      driver
    - `information_schema.columns` shows `data_type` `TIME(6) WITH TIME
      ZONE` and `datetime_precision` 6, and `GetObjects` the same
      `xdbc_type_name` and `xdbc_decimal_digits`
    - **Upgrading from v0.0.12 and earlier:** they read `TIME WITH TIME
      ZONE` as `TIME`, dropping a value's offset (`'10:00+02'` was
      `10:00:00`), and had no `TIMETZ`. Columns they created are `TIME`
      columns and stay so; `CREATE TABLE … AS SELECT c::timetz` or a new
      column makes a time with time zone column
  - **NaN and the infinities:** `REAL` and `DOUBLE PRECISION` hold `NaN`,
    `Infinity` and `-Infinity` (read from text in any case, also as `inf`),
    in every column, indexed or not (see "NaNs in the index"). As in
    Postgres, NaN is greater than every other number and equal to itself,
    in comparisons, `ORDER BY`, `GROUP BY`, `DISTINCT`, joins and
    aggregates, and as text (casts, `||`, JSON) they are `NaN`, `Infinity`
    and `-Infinity`, which were `NaN`, `+Inf` and `-Inf` up to v0.0.8.
    Arrow results hold the doubles themselves. `NUMERIC` has neither (in
    Postgres it has both): converting one to it fails with `cannot convert
    NaN to NUMERIC(10,2): NUMERIC values can't be NaN or infinite`, and
    nothing is written. A value beyond `REAL`'s range is an infinity
    (Postgres raises `value out of range: overflow`)
  - **Fractional seconds:** `TIME(p)` and `TIMESTAMP(p)` hold `p` digits,
    from 0 to 9 (6 without `p`; a `p` above 9 is 9, where Postgres reduces
    a `p` above 6 to 6; a negative `p` is an error, as in Postgres). Their
    Arrow unit is the one with the next number of digits: seconds for 0,
    milliseconds for 1 to 3, microseconds for 4 to 6 and nanoseconds for 7
    to 9, so a `TIMESTAMP(2)` is an Arrow `timestamp[ms]` whose values are
    whole hundredths. A value with more digits is rounded half away from
    zero, as Postgres rounds, in one step (`.1245` is `.12` in a
    `TIMESTAMP(2)`), when it is cast to the type or written to a column of
    it: `INSERT`, `UPDATE`, `MERGE`, `CREATE TABLE … AS`, defaults and bulk
    ingest (`'… 10:00:00.123456'` is stored as `.12` in a `TIMESTAMP(2)`).
    The rounding carries into the next second, minute, day or year
    (`'2024-12-31 23:59:59.5'` is `2025-01-01 00:00:00` in a
    `TIMESTAMP(0)`), and a `TIME(0)` of `23:59:59.9` is `24:00:00`, which
    Postgres allows (its Arrow value is 86,400 seconds, one past the end of
    Arrow's range for a time of day; pyarrow reads it as 00:00:00). As in
    Postgres, whose timestamps count from 2000-01-01, a timestamp before
    2000 exactly halfway rounds down (`'1999-12-31 23:59:59.5'` is
    `23:59:59`). The text, the Arrow value and what index queries compare
    with are the rounded value
    - **The type** keeps `p`: in the table's metadata, `information_schema`
      (`data_type` `TIMESTAMP(2)`, `datetime_precision` 2) and `GetObjects`
      (`xdbc_type_name` `TIMESTAMP(2)`, `xdbc_decimal_digits` 2). A column,
      a cast, a scalar subquery, and `CASE`, `COALESCE`, `NULLIF`,
      `GREATEST`, `LEAST` and set operations keep it, with the most digits
      of their inputs, so that no value is rounded (Postgres's type has no
      precision, which is 6, when they differ). So `CREATE TABLE … AS` and
      views keep the precision of a column or cast they select. Arithmetic
      (`ts + INTERVAL '1 second'`), functions, aggregates and a text
      literal in `CASE` or `COALESCE` have none, as in Postgres, and have
      the unit's digits (a `TIMESTAMP(3)` for a `TIMESTAMP(2)`)
    - `TIMESTAMP(p) '…'`, `TIMESTAMP(p) WITH TIME ZONE '…'`,
      `TIMESTAMPTZ(p) '…'` and `TIME(p) '…'` are the text cast to the type
    - Text with more than 6 digits read into a type of 6 or fewer is read
      as Postgres reads it, in microseconds with an exact half rounded to
      even, before it is rounded to the type (`'… 10:00:00.1234565'` is
      `.123456` in a `TIMESTAMP`, `'… 10:00:00.4999995'` is `10:00:01` in a
      `TIMESTAMP(0)`). Read into a type of 7 to 9 digits, and as a literal
      (`TIMESTAMP '… .1234565'`, a `TIMESTAMP(9)`), it is nanoseconds, which
      round half away from zero like any other value; outside the
      nanoseconds' range (1677 to 2262) it is microseconds, read as Postgres
      reads them
    - Text compared with a time or timestamp is read with the digits
      written (in microseconds unless the other side is in nanoseconds), as
      Postgres compares at full precision, so a `TIMESTAMP(0)` of `10:00:01`
      equals `'… 10:00:01'` but not `'… 10:00:00.5'`
    - Earlier versions truncated (`10:00:00.5` was `10:00:00` in a
      `TIMESTAMP(0)`); the values they stored don't change. v0.0.8 and
      earlier also kept 3, 6 or 9 digits for a `p` of 1 or 2, 4 or 5, 7 or
      8, and the column's type in their metadata is that: a `TIMESTAMP(2)`
      column they created stays a `TIMESTAMP(3)`, values and type

Tables can be qualified as `schema.table` or `redis.schema.table`, and
`pg_temp.table` is the connection's temporary table. Strings are written
`'…'` (`''` for a quote) or dollar-quoted, `$$…$$` or `$tag$…$tag$`, as in
Postgres. Schemas are key
namespaces (default `public`; `SET search_path` changes it for the
connection). There are no transactions (autocommit only; `BEGIN`, `COMMIT`
and `ROLLBACK` are accepted and do nothing, see "Transactions and settings"
above). `UPDATE`, `DELETE` and `MERGE` find their rows and compute and check
every change first (new values and casts, string lengths, `NOT NULL`, `CHECK`, MERGE's
one-change-per-row rule, `RETURNING`), so such an error leaves the table untouched. Then
they write, in pipelined batches of up to 1,000 rows (`MERGE`: updates, then
deletes, then inserts). If a write fails part-way, for example because the
connection drops, the batches already written stay written, and other
clients can see a partly applied statement.

## Options

| Option | Level | Meaning |
|-|-|-|
| `uri` | database | `redis://[user:pass@]host:port/db`, or `rediss://…` for TLS, with go-redis's query parameters (see [URI parameters](#uri-parameters)) |
| `username`, `password` | database | Credentials (override the URI) |
| `adbc.redis.address`, `adbc.redis.db` | database | Used when no URI is given |
| `adbc.redis.cluster` | database | `auto` (default) / `true` / `false`: OSS Cluster API client or single endpoint |
| `adbc.redis.default_schema` | database | Schema for unqualified names (default `public`). `SET search_path` and the ADBC current schema change it for one connection; `RESET search_path` goes back to it |
| `adbc.redis.aggregate_pushdown` | database, statement | `exact` / `all` / `none` |
| `adbc.redis.rename_rekey` | database, connection | `false` (default): `RENAME TO` only changes metadata. `true`: it also moves the rows and index to the new name's keys, in time proportional to the number of rows, and the table refuses changes meanwhile (see `ALTER TABLE`). The connection option overrides the database's |
| `adbc.redis.read_timeout` | database, connection | How long the client waits for each reply: `30s`, `10m`, … or a number of seconds; `0` for no timeout. Default `5m`, or the URI's `read_timeout` (see [Timeouts](#timeouts)). The connection option overrides the database's |
| `adbc.redis.write_timeout` | database, connection | How long the client waits to send each command, in the same format. Default: the read timeout, or the URI's `write_timeout` |
| `adbc.redis.ingest.index_columns` | statement | Comma-separated columns to index on bulk ingest (`*` = all indexable) |
| `adbc.redis.stream_results` | database, statement | `true` (default): [stream](#streamed-results) the results of queries that can be. `false`: build every result whole before `ExecuteQuery` returns. The statement option overrides the database's |
| `adbc.redis.stream_batch_rows` | database, statement | Rows per record batch of a streamed result (default 65,536). A result that fits in the first batch is returned whole. Also an integer option on the statement |
| `adbc.redis.time_zone` | database, connection | The session time zone, with the values `SET TIME ZONE` takes (default `UTC`; see Session time zone), and what `RESET timezone` and `SET TIME ZONE DEFAULT` go back to. The connection option overrides the database's and replaces a `SET`'s value; reading it gives the current value, as `SHOW timezone` does |

### Scale considerations

Scale tips: keep column names short (they are repeated in every HASH), raise
`hash-max-listpack-entries` / `hash-max-listpack-value` so small rows use the
compact encoding, and index only the columns you filter or aggregate on.

## Performance

### Benchmark setup

Measured with `go/examples/bench.py` (see below) on the sample data set
scaled to **100,000 `sales` rows × 10 columns** and 50 `customers`.
Setup: Apple M4 (10 cores, 16 GB), Docker Desktop, Redis 8.4.4 (Search
8.4.10) in Docker on the same machine. Times are the median of 5 runs after a
warm-up, measured in Python and including the Arrow transfer; "Redis
commands" are counted with `INFO commandstats` on the standalone server (a
cluster adds the shard fan-out, `_FT.AGGREGATE` / `_FT.CURSOR`). Numbers on a
laptop vary from run to run; treat them as orders of magnitude.

### Results

Bulk ingest (`adbc_ingest`): 100,000 rows in 1.6 s standalone (62,000 rows/s)
and 0.56 s on a 3-shard cluster (180,000 rows/s).

| Query | Rows out | Standalone (ms) | 3-shard cluster (ms) | Redis commands |
|-|-:|-:|-:|-:|
| Point lookup, `WHERE __rowid = N` | 1 | 1.7 | 1.0 | 2 |
| `COUNT(*)` with `ts >= TIMESTAMP … - INTERVAL '30 days'` | 1 | 1.4 | 1.0 | 2 |
| Indexed TAG filters + `ORDER BY … LIMIT 10` | 10 | 4.4 | 1.9 | 2 |
| `COUNT(*)` with an indexed range | 1 | 5.0 | 2.5 | 2 |
| `GROUP BY` with `COUNT` / integer `SUM` (index reduce) | 6 | 6.9 | 3.1 | 2 |
| Selective join (index lookup join) + `GROUP BY` | 8 | 25 | 27 | 6 |
| `COUNT(*)` on a view, filter pushed into its base table | 1 | 27 | 15 | 4 |
| Correlated scalar subquery for each of 50 customers | 50 | 31 | 28 | 54 |
| `UNION` of two indexed filters | 1,750 | 48 | 46 | 6 |
| Fetch 10% of the rows (indexed range) | 10,000 | 50 | 68 | 4 |
| `COUNT(*)` with `LIKE 'gi%'` (index prefix query, profiled first) | 1 | 62 | 64 | 5 |
| Filter on a non-indexed column (`notes LIKE '%x%'`) | 1 | 272 | 279 | 12 |
| Unfiltered join + `GROUP BY` (hash join over all rows) | 6 | 345 | 358 | 16 |
| `GROUP BY` with `AVG` of a `DOUBLE` (driver-side) | 6 | 463 | 522 | 12 |
| `GROUP BY` of an expression `SUM(quantity * unit_price)` | 5 | 494 | 549 | 12 |
| `GROUP BY DATE_TRUNC('month', ts)` | 12 | 499 | 542 | 12 |
| Full scan, `SELECT *` | 100,000 | 679 | 825 | 13 |

### Interpretation

What drives the numbers:

- **Queries the index answers on its own take milliseconds**, independent of
  table size: counts and ranges, TAG equality filters with `ORDER BY … LIMIT`,
  and `GROUP BY` with `COUNT` and integer `SUM`/`MIN`/`MAX`.
- **Reading rows costs about 3–7 µs per row.** Rows come back inside the
  `FT.AGGREGATE` cursor pages, up to 10,000 per page, so reading all 100,000
  rows takes a dozen commands and 0.3–0.8 s. Queries that need the driver to
  see every row pay this: aggregates the index can't compute exactly (`AVG`
  of doubles, expressions, `DATE_TRUNC` groups), filters on non-indexed
  columns, and unfiltered joins. Of the full scan's 0.68 s, Redis itself
  takes about 0.35 s to produce the rows (measured from inside the
  container); the rest is decoding, Arrow conversion, and Docker Desktop's
  port forwarding (about 15 ms per page). Up to v0.0.3 the driver fetched
  each row with its own pipelined `HMGET` instead, at about 30 µs per row
  (3.3–3.8 s for the full scan).
- **Selective joins are fast** because the smaller side is filtered in its
  index first and only matching rows of the other side are fetched. A
  correlated scalar subquery costs one indexed query per distinct outer
  value (about 0.6 ms each here).
- **`IN (SELECT …)` and correlated `EXISTS` / `IN` on equalities don't
  run per row** (`bench.py` has three such queries over the 100,000 rows).
  `order_id IN (SELECT …)` with 50,000 values takes 0.74 s: each row is
  checked against a hash set of the values. `EXISTS (SELECT … WHERE
  r.order_id = s.order_id AND …)` with 50,000 inner rows takes 0.87 s: it
  runs per outer row for the first 500 rows, then reads the inner side once
  as a hash semi-join. When the subquery has no rows it takes 2 ms, because
  the index then rules out every row. Without the hash set, each row was
  compared with every value (23 s), and without the semi-join the subquery
  ran once per outer row (100,000 `FT.AGGREGATE` commands, 22–23 s).
- **dbt's `delete+insert` with a composite key** (`where (k1, k2) in
  (select distinct k1, k2 from …)`) deleted 10,000 keys from a table of
  200,000 rows in about 1 s, standalone and on a 3-shard cluster, and
  inserted them in 0.4 and 0.15 s (measured separately with
  `TestSQLRowValueScale`: Redis 8.6.2 on a machine shared with other work,
  median of 4 runs, which ranged from 0.8 to 2.2 s). Every row is read and
  checked against a hash
  set of the keys. The per-key forms, `DELETE … USING` and `EXISTS`, also
  read every row and took 1.2–1.6 s. When a key column of the keys has at
  most 1,000 distinct values, the index fetches only the rows with those
  values, and the delete took 0.2–0.3 s.
- **Long literal row lists and keyset pagination** (`TestSQLRowValueScale`
  at 200,000 rows, standalone: the better of two runs, and one run of
  v0.0.12). Counting the rows in a `(k1, k2) IN (…)` list of 2,000
  scattered keys, more than an index union takes, took 0.6 s: every row is
  read and checked against a hash set of the list. Up to v0.0.12 each row
  was compared with each row of the list, and `IN` and `NOT IN` took 89 and
  56 s. With 1,000 keys the index fetches 4,280 rows, in 62 ms (0.56 s).
  Paging through a table of 200,000 rows 1,000 at a time with `WHERE (ts,
  id) > (?, ?) ORDER BY ts, id LIMIT 1000` took 11 s for the 200 pages, and
  a page of 100 rows near the end 12 ms: the index starts each page at the
  last page's `ts`. Up to v0.0.12 each page also read every row before it
  (86 s and 751 ms).
- **`ORDER BY` on values from 2^53 on** costs about as much as on smaller
  ones, as long as the rows don't tie (see "Ties in the sort vector"). On
  100,000 rows with `BIGINT` ids 2^60 + 7k (Snowflake-like; 36 share each
  double, so a `LIMIT` nearly always ends within a tie), `SELECT * … ORDER
  BY id DESC LIMIT 20` took 8.3 ms, against 7.9 ms in v0.0.12 (which could
  return the rows of a tie in the wrong order): one more `FT.AGGREGATE`
  reads the tie. With `ORDER BY id LIMIT 20 OFFSET 50000` it took 22 ms
  (20 ms), with an `FT.AGGREGATE` for the tie at each end and an
  `FT.SEARCH` count. Measured with Redis 8.6.2, standalone, on a machine
  shared with other work, median of 21 runs.
- **`GROUP BY`, `MIN` and `MAX` on values from 2^53 on, in the `all`
  mode,** cost the index's `FT.AGGREGATE` and then the driver's, which
  reads every row as in the `exact` mode (see "Aggregate pushdown"). On
  100,000 rows with those Snowflake-like ids, `SELECT id, COUNT(*) …
  GROUP BY id` took 400 ms, against 382 ms in the `exact` mode, and `SELECT
  g, COUNT(*), MIN(id), MAX(id) … GROUP BY g` (6 groups) 294 ms (279 ms).
  v0.0.12 took 25 and 7.7 ms, and returned 2,735 groups for the 100,000
  ids and rounded `MIN`s and `MAX`es. Groups, `MIN`s and `MAX`es of
  smaller values still take one `FT.AGGREGATE`, as before. Measured with
  Redis 8.6.2, standalone, on a machine shared with other work, median of
  21 runs.
- **Queries that can't return rows read nothing**, only the tables'
  metadata. On a table of 152,686 rows (measured separately, best of 3,
  standalone), dbt's `select * from (<model>) as __dbt_sbq where false
  limit 0` over a model with two `GROUP BY`s and a join takes 1.2 ms, and
  the model's `dbt run --empty` form (each ref read as `(select * from t
  where false limit 0)`) 1.0 ms. Up to v0.0.6 they ran in full, in 1.0 s
  (as long as the model itself) and 2.7 s; `select * from t where false
  limit 0` took 1.4 s, and now 0.5 ms.

### Query performance factor (QPF)

Higher QPF did **not consistently improve performance** in the local tests
below. QPF adds query compute capacity per shard in Redis Software and Redis
Cloud; its benefit depends on the query, concurrency, and available CPU.
See [Redis's QPF documentation](https://redis.io/docs/latest/operate/oss_and_stack/stack-with-enterprise/search/query-performance-factor/).

On October 6, 2026, we ran 108 trials against a disposable single-shard Redis
Software database: standard (QPF disabled), 2×, and 4× QPF; concurrency 1 and
16; six workloads; three repeats per combination. Each trial sent requests
for three seconds and waited for outstanding requests to finish. The data
contained 100,000 hashes, each with a 512-byte payload. The database used
Redis 8.6.2 and Search 8.6.11, with RAM storage and no replica. QPF settings
were verified through the management API after each change.

At **16 concurrent clients**, median trial throughput relative to standard
was as follows. Values above 1.00× mean higher throughput.

| Direct Redis workload | 2× QPF | 4× QPF |
|---|---:|---:|
| `FT.SEARCH`, text count | 0.63× | 0.90× |
| `FT.SEARCH`, tag filter and small projection | 0.84× | 0.81× |
| `FT.SEARCH`, 100 full documents | 1.07× | 0.86× |
| `FT.AGGREGATE`, group and sum using `SORTABLE` values | 0.99× | 1.16× |
| `FT.AGGREGATE`, same group and sum with `LOAD` | 0.53× | 0.97× |
| `FT.SEARCH`, numeric range count | 0.61× | 0.82× |

The indexed aggregation's median trial p95 latency fell from 81.36 ms at
standard QPF to 68.55 ms at 4×. Aggregation with `LOAD` showed no consistent
throughput gain. Single-client results were also mixed. Query results were
checked across QPF settings, both aggregation variants returned identical
sums, and all 108 trials completed with zero command errors.

**These measurements do not establish production QPF speedups.** The local
Docker host had 10 virtual CPUs and substantial, varying CPU load from
another Redis test container. Throughput varied widely across repeats, and
some slow aggregation trials had few samples. These are direct
`FT.SEARCH`/`FT.AGGREGATE` measurements, not end-to-end ADBC SQL or Arrow
transfer benchmarks. This run does not support increasing QPF by default;
compare throughput and p95 latency with representative queries on an
isolated host before choosing a setting.

The [full report](benchmarks/qpf/report.md) includes absolute throughput,
latency, run-to-run ranges, exact queries, and reproduction commands.
[Raw results](benchmarks/qpf/results.json) and the
[benchmark script](benchmarks/qpf/run.py) are included.

### Potential optimizations

Known ways to make the slow cases faster (not done yet):

- `COUNT(*)` with a `LIKE` prefix, and `COUNT(*)` on a lazy view, still read
  the matched rows to re-check them, although the index has already matched
  them exactly; the index could answer them alone.
- Keyset pagination with a small `LIMIT` still sorts every row from the
  bound on and reads a whole cursor page (up to 10,000 rows) per page: the
  row comparison is checked on the rows, which keeps the `LIMIT` out of the
  index. The first cursor page could ask for only as many rows as the
  `LIMIT`.
- A `LIMIT` that ends within a tie of large `NUMERIC` values, or of a key
  after one that doesn't round (`ORDER BY category, id`), asks the index
  again with more rows, which sorts every match again; the driver could
  read the tie by value, as it does for an integer first key. Many rows
  with the same large value make one long tie, which such a `LIMIT` reads
  whole.
- In the `all` mode, `MIN` and `MAX` of `BIGINT`s from 2^53 on read every
  row, although only the rows that tie with the index's answer
  (`@c:[v v]`) can hold the exact one; the driver could read those, as an
  `ORDER BY … LIMIT` reads a tie.

### Reproduce the benchmark

From `go`, with Redis running:

```bash
REDIS_URI=redis://localhost:6379/0 uv run --project validation --with redis python examples/bench.py --rows 100000
```

The script **replaces** the `sales` and `customers` tables with benchmark data;
run `examples/load_sample.py` afterwards to get the quick-start data back.

## Building and testing

### Validation suite

See the [Quick start](#quick-start) for building. To run the ADBC validation
suite against the local Redis:

```bash
cd go/validation
REDIS_URI=redis://localhost:6379/0 uv run pytest -v tests/
```

The suite accepts any Redis 8.x server, so it can also target a remote or
Redis Cloud database. Use `rediss://` if the database requires TLS, and use
an empty database: the suite leaves its test tables (`test_*`, `getobjects*`,
`statistics*`) behind.

```bash
REDIS_URI='rediss://default:<password>@<host>:<port>/0' uv run pytest -v tests/
```

### Go tests

SQL features beyond the validation suite (CASE, HAVING, CTAS, …) have Go
integration tests with exact expected results. They need a running Redis and
are skipped when `REDIS_URI` is unset (from `go`):

```bash
REDIS_URI=redis://localhost:6379/0 go test -run TestSQL ./...
```

To run every test, pass `-p 1` (as `make test` does), so the driver's
package and its commands' packages don't test against the server at once. Any
connection that opens rolls back an abandoned move, including the ones the
driver's rename tests set up and then expect to find.

```bash
REDIS_URI=redis://localhost:6379/0 go test -p 1 ./...
```

`TestSQLRowValueScale` times dbt's `delete+insert` with a composite key
(with `-v`) on 20,000 rows; `REDIS_ROW_VALUES_ROWS=200000` runs it on
200,000 (a multiple of 200), as measured under Performance.

Some tests need more:

- **The ACL tests** (`TestACL…`) create ACL users `it_acl_*` and delete them
  afterwards, so the user needs the `ACL` command. To run the whole suite as
  a user with only the driver's commands (see [ACL users](#acl-users)), set
  `REDIS_URI` to that user and `REDIS_ADMIN_URI` to one that can do
  everything: the tests' own checks use it (`KEYS`, `ACL`, …).
- **`TestTimeoutServerPaused`** pauses the server (`CLIENT PAUSE`, on one
  shard of a cluster) for about half a minute, so it only runs with
  `REDIS_PAUSE_TESTS=1`. Run it against a test server only.

### Redis Cluster

To test against a local 3-shard Redis Cluster (from `go`):

```bash
docker compose --profile cluster up --detach --wait redis-cluster
```

```bash
cd validation && REDIS_URI=redis://localhost:7001/0 uv run pytest -v tests/
```

### Redis Flex

To test against Redis Flex, `flex-setup.sh` (from `go`) starts a
single-node Redis Software cluster with flash storage in Docker. It then
creates a Flex database with Search on it, and prints the database's URI.
The cluster uses about 1.5 GB of memory and the image's trial license. Its
"flash" is a directory inside the container, and its admin API (port 9443)
listens on localhost only, with fixed test credentials.

```bash
./flex-setup.sh
```

The driver refuses Flex databases (see
[Server requirements](#server-requirements)), and `TestFlexRefused` checks
that it does so without writing to the database. The test is skipped unless
`REDIS_FLEX_URI` is set:

```bash
REDIS_FLEX_URI=redis://localhost:12000/0 go test -count=1 -run TestFlex -v ./...
```

Remove the cluster with `docker compose --profile flex down redis-flex`.
Name the service: without it, `down` also removes the `redis` container.

#### Verified local result

On 2026-10-06, `TestFlexRefused` passed against Redis Software `8.2.0-78`,
Redis `8.6.2`, and Search `8.6.11`. The driver returned ADBC
`NotImplemented` with the expected Flex message and left the key count
unchanged. Direct probes confirmed that `FT.AGGREGATE`, `NUMERIC`, and
`SORTABLE` fields were unavailable. Temporary probe keys and indexes were
removed.

#### Redis Cloud Pro Flex

Run the same test from `go` using an authenticated database URI. The Redis
Cloud account and user API keys authenticate the management API; use the
database password for the Redis connection. Keep the URI in an environment
variable and out of source control. For example, in a shell with history
disabled or using a secret manager:

```bash
export REDIS_FLEX_URI='redis://default:<url-encoded-password>@<host>:<port>/0'
go test -count=1 -run TestFlex -v ./...
unset REDIS_FLEX_URI
```

The live Cloud run tested a newly created 250 GB Flex database in GCP
`us-east4`, on Redis `8.6` with 10% RAM. The database used
1,000 operations/second, no replication or
persistence, RESP3, no TLS, and a single proxy endpoint. The driver uses
RESP2. The 250 GB dataset was the minimum accepted by this subscription.

The authenticated run failed `TestFlexRefused` with
`ERR unknown command 'FT._LIST'`. This test expects the later Flex-specific
refusal, so a missing Search module is a test failure rather than a passing
Flex compatibility check. On the new database, `PING` returned `PONG`, but
`COMMAND INFO` found no registration for `FT._LIST`, `FT.CREATE`,
`FT.SEARCH`, or `FT.AGGREGATE`. No Cloud probe keys or indexes were created.

[Redis's documentation](https://redis.io/docs/latest/operate/rc/databases/create-database/create-flex-database/)
requires enabling a Search-on-Flex beta flag for the Pro subscription before
creating a database with Search on tiered storage. It does not specify the
flag's exact menu location. `Setup → Advanced options → Redis Flex` enables
tiered storage; it does not confirm Search beta enablement. If the beta
control is absent, ask Redis Support to confirm eligibility and the
enablement procedure. These tests did not independently verify the
subscription's beta entitlement or exercise a Search-enabled Cloud database.

The disposable Cloud database was left active pending explicit deletion
approval. The API dry run estimated approximately USD 1.43/hour for its
shape. See the [report](docs/redis-flex-compatibility.md) for full settings
and the distinction between live results and documented beta limitations.

### Cross-platform builds and releases

To build the Linux library (on Linux or a Mac, with Docker), run
`build-linux.sh` from `go` with `amd64` or `arm64`. It writes
`build/linux-<arch>/libadbc_driver_redis.so`. A foreign architecture runs
under emulation and is much slower.

```bash
./build-linux.sh arm64
```

The script builds in an AlmaLinux 8 container, against glibc 2.28: the
oldest glibc among the platforms Redis Software supports. So one library
loads on all of them, and the script fails if it would need a newer glibc.

Release binaries come from `.github/workflows/release.yml`. When a release is
published, it builds the Linux (amd64, arm64) and macOS (arm64) libraries,
runs the Go tests and the validation suite against Redis on both Linux
builds (standalone and a 3-shard cluster), and then runs the validation suite
on each Linux tarball under Ubuntu 20.04, 22.04 and 24.04, AlmaLinux 8 and 9
(RHEL 8 and 9), and Amazon Linux 2023. Only then does it attach the tarballs
and their `.sha256` files to the release. GitHub's macOS runners can't run the
Redis containers, so the macOS build is only checked to load and to pass the
Go unit tests. To rebuild an existing release's assets, run the workflow
by hand with its tag:

```bash
gh workflow run release.yml -f tag=v0.0.4
```

The run builds and tests the tag's own code. If a Go test at that tag is
wrong on one platform (not the driver), `skip` (a `go test -skip` pattern)
leaves it out. v0.0.8 and v0.0.9 got their binaries this way: their
`linux-amd64` jobs failed `TestScalarTrigonometry` and `TestSQLNaNAggregates`
(fixed after them by #121 and #125).

```bash
gh workflow run release.yml -f tag=v0.0.8 -f skip=TestScalarTrigonometry
```

### Python usage

Use the driver from Python through the ADBC driver manager:

```python
import adbc_driver_manager.dbapi as dbapi
conn = dbapi.connect(driver="go/build/libadbc_driver_redis.so",
                     db_kwargs={"uri": "redis://localhost:6379/0"})
cur = conn.cursor()
cur.adbc_ingest("my_table", arrow_table, mode="create")  # any pyarrow.Table
cur.execute("SELECT country, SUM(quantity) FROM sales WHERE quantity > 10 GROUP BY country")
print(cur.fetch_arrow_table())
```

The C FFI shim in `go/pkg` was generated with driverbase-go's `ffitemplate`
(`go run main.go -prefix Redis -driver <path>/go`).
