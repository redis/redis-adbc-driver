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

# Arrow Database Connection (ADBC) driver for Redis

An [ADBC](https://arrow.apache.org/adbc/) driver built on
[driverbase-go](https://github.com/adbc-drivers/driverbase-go) and validated
with the [ADBC driver validation suite](https://github.com/adbc-drivers/validation).
It runs SQL over Redis HASHes and the Query Engine, returns Arrow record batches,
and bulk-ingests Arrow data through a native ADBC library.

**Works best with Redis Search (Query Engine) enabled and query performance
factor (QPF) set to 4× where supported.** Search is required;
4× QPF is recommended for concurrent query performance. See
[server requirements](docs/driver-reference.md#server-requirements) for setup guidance.

For repository context and the development artifact workflow, see the
[AI-native SDLC records](docs/sdlc/README.md).

## Contents

- [Why a Redis ADBC driver?](#why-a-redis-adbc-driver)
- [Compatibility and validation](#compatibility-and-validation)
- [Apache Arrow feature matrix](#apache-arrow-feature-matrix)
- [Redis Software feature matrix](#redis-software-feature-matrix)
- [Data tools and integration status](#data-tools-and-integration-status)
- [Quick start](#quick-start)
- [dlt integration](docs/python-integrations.md#dlt)
- [Driver reference](docs/driver-reference.md): server requirements, existing HASH collections, architecture, SQL, options, performance, building and testing

## Why a Redis ADBC driver?

Driver users can query Redis with familiar SQL and move data through Apache Arrow,
making it easier to connect operational data to the tools their teams already use.

**What this means for driver users:**

- **Use popular data engineering tools:** Bring Redis data into workflows with
  Dagster, Airflow, Python, pandas, Polars, and other tools that can consume ADBC
  drivers. Some tools require custom tasks or integration code; see the
  [integration status guide](docs/data-engineering-adbc-support.md) for details.
- **Analyze data where it lives:** Query data directly in Redis using products
  like [Apache Superset](docs/python-integrations.md#sqlalchemy-and-superset)
  and [DuckDB](docs/duckdb.md), reducing the need to export it elsewhere before
  analysis.
- **Less custom integration code:** Use standard SQL and Arrow interfaces to
  reduce application-specific reads and data conversions.
- **More value from existing data:** Bring eligible Redis HASH collections into
  SQL workflows [in place](docs/driver-reference.md#adopt-hashes-in-place),
  without copying their rows.
- **Efficient data movement:** Stream typed Arrow results and bulk-load Arrow
  batches, with supported filters and aggregations pushed down to Redis's
  Query Engine.

- **Enable Flight SQL and JDBC integration.** The
  [Redis Arrow Flight server](https://github.com/redis/redis-apache-arrow-flight)
  uses this driver as its Redis SQL/Arrow backend. JDBC tools such as DBeaver can
  access that separately deployed server through the
  [Flight SQL JDBC driver](https://arrow.apache.org/java/current/flight_sql_jdbc_driver.html).
  See its [client examples](https://github.com/redis/redis-apache-arrow-flight#client-examples)
  and [client compatibility matrix](https://github.com/redis/redis-apache-arrow-flight/blob/main/docs/client-compatibility.md)
  for setup and tested compatibility.

A simpler path from application data to exploration, dashboards, and data
pipelines—with less glue code to build and maintain.

The intended use cases are operational reporting, data exploration, and Arrow
data pipelines over supported HASH data. Redis with the Query Engine and RESP3
is required. SQL follows the driver's documented subset; writes autocommit, and
transactions and atomic source snapshots are unavailable. Native Redis clients
provide the broader Redis command and data-structure interface. See
[server requirements](docs/driver-reference.md#server-requirements) and the
feature matrices below when choosing an access path.

## Compatibility and validation

Tested Redis editions:

- Redis Software 8.2.0-78.18 (Redis 8.6.2)
- Redis Open Source 8.6.2 (standalone and cluster)
- Redis Cloud, including Redis Cloud Pro (Redis 8.6.2)

See [compatibility and validation details](docs/compatibility.md) for tested
versions, configurations, results, and limitations.

## Apache Arrow feature matrix

🟢 Supported · 🟡 Partial or restricted · 🔴 Unsupported · ⚪ N/A (outside driver scope)

This matrix describes the current source tree, not every published wheel. It
covers the [Apache Arrow format and interfaces](https://arrow.apache.org/docs/format/Columnar.html)
and the [ADBC API](https://arrow.apache.org/adbc/current/format/specification.html);
these are separate capabilities. Redis stores rows as HASHes; Arrow is the
client interchange format. N/A identifies responsibilities of another service or
execution engine; it does not describe a missing driver capability. Tracking links
for those rows cover scope or companion integration decisions.

| Feature | Support | Driver behavior and limits | Tracking |
|---|---|---|---|
| Columnar record batches, schemas and NULL values | 🟢 Supported | Queries return typed Arrow batches with validity information. | — |
| Arrow C Data and C Stream interfaces | 🟢 Supported | The native ADBC boundary exchanges Arrow schemas, arrays and `ArrowArrayStream` results/bindings. | — |
| Arrow bulk ingestion | 🟢 Supported | Bind a batch or stream; create, append, replace and create-or-append modes are available. Writes autocommit. | — |
| Arrow IPC stream and file formats | 🟢 Supported | Native import/export through statement options and local files; [IPC contract and examples](docs/arrow-ipc.md). | — |
| Incremental query results | 🟡 Partial | Eligible indexed single-table queries and eligible `UNION ALL` trees fetch pages as batches are consumed. Blocking plans can materialize results; this is finite streaming. See [streamed results](docs/driver-reference.md#streamed-results). | [#198](https://github.com/redis/redis-adbc-driver/issues/198) |
| Zero-copy interchange | 🟡 Partial | Arrow buffers cross the in-process C boundary; Redis replies still require decoding/conversion. No end-to-end zero-copy storage or network path. | [#199](https://github.com/redis/redis-adbc-driver/issues/199) |
| Boolean, signed integers, floating point, UTF-8 and binary | 🟢 Supported | Standard SQL/Arrow mappings; int8 and float16 inputs widen to int16 and float32. | — |
| Unsigned integers | 🟡 Partial | Ingested as signed SQL integers; uint64 values above `INT64_MAX` fail. Unsigned physical types are not preserved. | [#200](https://github.com/redis/redis-adbc-driver/issues/200) |
| Decimal32/64/128/256 | 🟡 Partial | Inputs map to SQL NUMERIC, with precision up to 76 digits; results use Decimal128 or Decimal256. | [#201](https://github.com/redis/redis-adbc-driver/issues/201) |
| Date, time and timestamp | 🟡 Partial | Date64 normalizes to Date32; timestamps with a time zone normalize to UTC. Original physical encodings and zone names are not all preserved. | [#202](https://github.com/redis/redis-adbc-driver/issues/202) |
| Intervals and durations | 🟡 Partial | Inputs normalize to SQL INTERVAL; output uses MonthDayNanoInterval. | [#203](https://github.com/redis/redis-adbc-driver/issues/203) |
| Large/view strings and binary; fixed-size binary | 🟡 Partial | Accepted on input, normalized to ordinary string/binary output. Original layouts are not preserved. | [#204](https://github.com/redis/redis-adbc-driver/issues/204) |
| Dictionary encoding | 🟡 Partial | Input dictionaries decode to their supported value type; dictionary encoding is not preserved in table results. | [#205](https://github.com/redis/redis-adbc-driver/issues/205) |
| List and LargeList | 🟡 Partial | One-dimensional SQL arrays of supported elements; output uses List. Nested lists are rejected. | [#206](https://github.com/redis/redis-adbc-driver/issues/206) |
| Struct | 🟡 Partial | SQL record expressions can return Struct arrays; arbitrary Struct ingestion is unsupported. | [#207](https://github.com/redis/redis-adbc-driver/issues/207) |
| Map, Union, FixedSizeList, ListView and run-end encoding | 🔴 Unsupported | No general ingestion or table-type mapping for these Arrow layouts. | [#208](https://github.com/redis/redis-adbc-driver/issues/208) |
| Arbitrary Arrow extension types | 🔴 Unsupported | No general extension-type ingestion or round-trip contract. | [#209](https://github.com/redis/redis-adbc-driver/issues/209) |
| SQL execution, prepared statements and Arrow parameter binding | 🟢 Supported | Supports `Prepare`, `Bind`, `BindStream`, parameter schemas and result-schema discovery for supported SQL. | — |
| ADBC metadata discovery | 🟡 Partial | Driver info, objects, table schemas/types and approximate managed-table row counts are available. Exact/column statistics and UNIQUE/FOREIGN KEY metadata remain unavailable. See [statistics](docs/adbc-statistics.md). | [#210](https://github.com/redis/redis-adbc-driver/issues/210) |
| ADBC partitioned reads | 🟡 Partial | Portable row-range descriptors cover a restricted SELECT subset; retained-result mode supports broader SQL with quotas/expiry. No shared source snapshot. Rebuild from source; see [partitioned reads](docs/partitioned-reads.md). | [#211](https://github.com/redis/redis-adbc-driver/issues/211) |
| Statement cancellation | 🟡 Partial | Cancels driver work and interrupts execution-owned socket waits, including blocked cursor pages/pipelines. Remote server work may continue; sorts finish before checking cancellation. See [cancellation](docs/driver-reference.md#cancelling-a-statement). | [#212](https://github.com/redis/redis-adbc-driver/issues/212) |
| Transactions and rollback through ADBC | 🔴 Unsupported | Autocommit only; disabling autocommit is rejected. | [#113](https://github.com/redis/redis-adbc-driver/issues/113) |
| Substrait plans | 🔴 Unsupported | `SetSubstraitPlan` returns NotImplemented. | [#213](https://github.com/redis/redis-adbc-driver/issues/213) |
| Arrow Flight / Flight SQL endpoint | ⚪ N/A | A Flight endpoint belongs to a separately deployed service, such as the [Redis Flight server](https://github.com/redis/redis-apache-arrow-flight). This native ADBC driver supplies an in-process client interface. | [#214](https://github.com/redis/redis-adbc-driver/issues/214) |
| Continuous subscriptions / CDC | ⚪ N/A | Durable change feeds and snapshot-to-live handoff need a separately designed source/service contract. Finite SQL query results are the driver's current interface; [#196](https://github.com/redis/redis-adbc-driver/issues/196) evaluates where CDC should live. | [#196](https://github.com/redis/redis-adbc-driver/issues/196) |
| Arrow C Device interface | 🔴 Unsupported | No device-buffer ingestion or Arrow C Device interchange contract. Device-buffer interoperability is separate from executing SQL on a GPU. | [#215](https://github.com/redis/redis-adbc-driver/issues/215) |
| GPU query execution | ⚪ N/A | GPU execution is the responsibility of a compute engine consuming Arrow results; this driver runs its SQL evaluator on the CPU and delegates eligible work to Redis Search. | — |

Type mappings are implemented in [types.go](go/types.go) and
[arrowconv.go](go/arrowconv.go); ADBC methods in
[statement.go](go/statement.go) and [connection.go](go/connection.go).
Supported interchange does not imply support for every Arrow layout or every
SQL operator. See the [SQL reference](docs/driver-reference.md#supported-sql)
for type and execution details.

## Redis Software feature matrix

🟢 Supported through the driver · 🟡 Partial or restricted ·
🔴 Unsupported driver capability/compatibility · ⚪ N/A (outside driver scope)

These statuses describe **this driver's SQL/Arrow interface**, not whether Redis
Software offers a feature. They reflect current source and existing validation
records as of **2026-10-22**; no new deployment tests were run for this table.
N/A identifies work owned by server administration or companion services. It is
not a compatibility failure. Red identifies a missing driver capability or an
unsupported deployment; yellow identifies limits in an applicable driver path.

| Redis Software feature | Support | Driver behavior and limits |
|---|---|---|
| Query Performance Factor (QPF) | 🟢 Supported | Search queries use the server's configured QPF. Development and tests require verified **4× QPF**; `make -C go redis-up` verifies active scaling factor 4, six Search workers, `mnp` routing and 32 connections. QPF is separate from driver query-worker settings and does not promise a 4× query speedup. [Configuration](https://redis.io/docs/latest/operate/oss_and_stack/stack-with-enterprise/search/query-performance-factor/), [driver performance](docs/driver-reference.md#query-performance-factor-qpf). |
| RESP3 | 🟢 Required | Required for single-endpoint and Cluster API clients, including every node connection; explicit `protocol=3` is accepted. |
| RESP2 compatibility | 🔴 Unsupported | `?protocol=2` is rejected; RESP3 is required. See [URI parameters](docs/driver-reference.md#uri-parameters). |
| Redis Query Engine / Search | 🟢 Supported | Required at connection time. The driver creates HASH indexes and uses Search for supported SQL filters, ordering and aggregation, with driver-side execution where needed. Vector and full-text SQL operations are assessed separately below. [Execution](docs/driver-reference.md#query-execution). |
| Redis HASH tables | 🟢 Supported | Driver-managed HASH rows support SQL reads/writes, DDL and Arrow bulk ingestion. Indexes accelerate queries; HASH values remain authoritative. [Layout](docs/driver-reference.md#rows-and-indexes). |
| Existing application HASHes and indexes | 🟡 Partial | `redis-adbc` can inspect, copy or adopt supported HASH collections; configured foreign indexes enable discovery. Layout, ownership and mutation restrictions apply. [Existing HASH collections](docs/driver-reference.md#existing-hash-collections-redis-adbc). |
| Single endpoint / Redis Software proxy | 🟢 Supported | Connect to a supported Search-enabled RAM database through its Redis endpoint; the proxy routes database operations. |
| Sharding / OSS Cluster API | 🟢 Supported | Auto-detects the Cluster API or accepts `adbc.redis.cluster=true`; metadata shares one hash slot and table searches are coordinated across shards. This is client connectivity, not a shard-management API. [Cluster behavior](docs/driver-reference.md#prerequisites). |
| TLS and private certificate authorities | 🟢 Supported | `rediss://` verifies server certificates; `adbc.redis.tls_ca_file` supplies explicit trust roots, including cluster nodes and independent partition workers. [TLS evidence](docs/partitioned-reads.md#tls-and-independent-worker-validation). |
| Mutual TLS with client certificates | 🔴 Unsupported | No public driver options configure a client certificate/private key; server-certificate verification alone does not provide mutual TLS. |
| Password / ACL-user authentication | 🟢 Supported | URI credentials or username/password options authenticate Redis connections. [ACL requirements](docs/driver-reference.md#acl-users). |
| Role-based access control (RBAC) | 🟢 Supported | Uses administrator-provisioned Redis users and command/key permissions, including restricted partition workers. SQL schemas are not security boundaries; Search DDL requires the documented permissions. Role provisioning is a server administration task. [Permissions](docs/driver-reference.md#search-commands-and-key-patterns). |
| Replication | ⚪ N/A | Redis Software manages primary/replica synchronization and replica configuration. The driver connects to the database endpoint; replica read routing is assessed separately below. [Server replication](https://redis.io/docs/latest/operate/rs/databases/durability-ha/). |
| Automatic failover (client recovery) | 🟢 Supported | Uses go-redis connection recovery and Cluster routing for supported Redis Software proxy / Cluster API connections. Applications must handle interrupted operations, as with standard Redis clients. Dedicated live-failover validation is not recorded. [Retry and timeout limits](docs/driver-reference.md#timeouts), [official client guidance](https://redis.io/docs/latest/develop/clients/go/error-handling/). |
| Read scaling through replicas | 🔴 Unsupported | `read_only=true`, `route_by_latency=true` and `route_randomly=true` are rejected because replicas can lag the driver's writes. |
| Persistence configuration (AOF / snapshots) | ⚪ N/A | Persistence policy is configured in Redis Software, outside the ADBC interface. Stored driver data follows that server policy; [retained partition control state](docs/retained-partition-storage.md) has additional durability requirements. [Server settings](https://redis.io/docs/latest/operate/rs/databases/configure/database-persistence/). |
| Backup, restore and disaster recovery | ⚪ N/A | These are server/operator responsibilities. Restore must keep table metadata, HASHes and Search definitions consistent. [Server administration](https://redis.io/docs/latest/operate/rs/databases/configure/). |
| Active-Active geo-distribution | 🔴 Unsupported / unvalidated | No supported multi-region contract for concurrent DDL, metadata/row-ID allocation, writes or partition consistency. Redis Software supports Search on Active-Active, but indexes must be created per instance; driver compatibility cannot be inferred from command availability. The driver does not guarantee automatic rejection of an Active-Active endpoint. [Redis Active-Active Search](https://redis.io/docs/latest/operate/oss_and_stack/stack-with-enterprise/search/search-active-active/). |
| Redis Flex / Auto Tiering | 🔴 Unsupported | The driver's Search requirements are incompatible with the documented Flex path; it refuses detected Flex databases. [Requirements and evidence](docs/driver-reference.md#redis-flex-databases-are-not-supported-yet). |
| JSON document collections / ON JSON indexes | 🔴 Unsupported | Table storage and collection adoption target HASHes; RedisJSON document collections are outside that path. This is separate from SQL JSON functions. [#65](https://github.com/redis/redis-adbc-driver/issues/65). |
| Vector and full-text SQL operations | 🔴 Unsupported | No supported SQL operations for Redis vector KNN/range search or full-text relevance scoring. Ordinary SQL string predicates do not provide those query semantics. |
| Streams, Pub/Sub and keyspace notifications | ⚪ N/A | Native messaging and event-subscription APIs belong to Redis clients or a companion service, outside this SQL/Arrow interface. Finite Arrow result streaming is separate; [CDC placement is tracked in #196](https://github.com/redis/redis-adbc-driver/issues/196). |
| SQL tables over other Redis data types | 🔴 Unsupported | No SQL/Arrow table mapping for application lists, sets, sorted sets, time-series or probabilistic structures. Internal use of a Redis data type does not expose it as an application table. |
| Cluster and database administration | ⚪ N/A | Provisioning, resharding, upgrades, license management and monitoring configuration belong to the [Redis Software management tools](https://redis.io/docs/latest/operate/rs/databases/configure/). |

Implementation references: [connection and protocol configuration](go/database.go),
[TLS trust configuration](go/tls.go), [QPF verification](go/software-qpf.sh), and
[the detailed driver reference](docs/driver-reference.md). Green entries do not
claim every server version or failover scenario has been validated.

## Data tools and integration status

Examples of working integrations in this repository:

| Tool | Example workflow | Setup and examples |
|---|---|---|
| pandas | Read SQL results into DataFrames and write or append DataFrames to Redis tables. | [pandas reads and writes](docs/python-integrations.md#pandas-writes-and-round-trips) |
| Polars | Query Redis with `read_database_uri(..., engine="adbc")` and bulk-load DataFrames through ADBC. | [Python installation and Polars example](docs/python-integrations.md#install-and-discover-the-driver) |
| DuckDB | Attach Redis tables with `adbc_scanner` to query, join, and ingest data; stream SELECT results with the standalone `redis_adbc` extension. | [DuckDB guide](docs/duckdb.md) |
| Apache Superset | Browse Redis schemas and tables, run SQL in SQL Lab, and build charts through the SQLAlchemy integration. | [Superset setup](docs/python-integrations.md#sqlalchemy-and-superset) |
| Dagster | Query Redis from assets with `dagster-adbc`, or store and load Arrow/DataFrame asset outputs with the Redis ADBC IO manager. | [Dagster examples](docs/python-integrations.md#dagster) |
| Apache Airflow | Bulk-load Arrow batches and query grouped results in a TaskFlow DAG or through the official `AdbcHook` with explicit autocommit. | [Airflow DAG and hook examples](docs/python-integrations.md#apache-airflow) |

See [ADBC support across 22 data engineering tools](docs/data-engineering-adbc-support.md)
for validation evidence, integration limits, and additional tools, including
candidate integrations that have not been tested.

## Quick start

Download a prebuilt driver, start Redis Software, and load the sample dataset.
You need Git, Docker with Compose, `curl`, `jq`, Make, and Python 3.12+.

### 1. Download the release

Clone the repository for its Redis startup script and sample data loader:

```bash
git clone --depth 1 https://github.com/redis/redis-adbc-driver.git
cd redis-adbc-driver
```

Download the [v0.0.18 native driver](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.18)
for your platform and unpack it into `go/build`, where the examples find it:

```bash
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) platform=darwin-arm64 ;;
  Linux-x86_64) platform=linux-amd64 ;;
  Linux-aarch64|Linux-arm64) platform=linux-arm64 ;;
  *) echo "Choose a supported platform from the release page."; exit 1 ;;
esac
curl -fL -o /tmp/redis-adbc-driver.tar.gz \
  "https://github.com/redis/redis-adbc-driver/releases/download/v0.0.18/adbc_driver_redis-v0.0.18-${platform}.tar.gz"
mkdir -p go/build
tar -xzf /tmp/redis-adbc-driver.tar.gz --strip-components=1 -C go/build
python3 -m venv .venv
. .venv/bin/activate
python -m pip install adbc-driver-manager pyarrow
```

The native releases support macOS Apple Silicon and Linux x86-64/ARM64
(glibc 2.28+). For Python applications, you can also
[install the packaged driver](docs/python-integrations.md#install-and-discover-the-driver).
To compile your own library, see [Building from source](docs/building.md).

### 2. Start Redis Software

```bash
make -C go redis-up
```

This starts a disposable local Redis Software database at
`redis://localhost:6379/0`, enables Redis Search (Query Engine), and verifies
4× QPF. Keep this terminal's virtual environment active for the following steps.

### 3. Load the sample dataset

```bash
python go/examples/load_sample.py
```

This creates 50 customers and 10,000 sales rows in the `public` schema.
It replaces existing `customers` and `sales` tables in the target database.

### 4. Query Redis with SQL

```bash
python go/examples/sql.py "SELECT COUNT(*) FROM sales"
python go/examples/sql.py "SELECT country, SUM(quantity) AS units FROM sales GROUP BY country ORDER BY units DESC"
```

Run `python go/examples/sql.py` for an interactive SQL prompt. See the
[sample dataset walkthrough](docs/sample-dataset.md) for more queries and
instructions for inspecting the Redis data.

When finished, stop and remove the disposable Redis Software database:

```bash
make -C go redis-down
```
