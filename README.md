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

**What this means for customers:**

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

- Redis Software
- Redis Open Source (standalone and cluster)
- Redis Cloud (including Redis Cloud Pro)

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

See [ADBC support across 22 data engineering tools](docs/data-engineering-adbc-support.md)
for an ecosystem comparison covering Redis ADBC, Redis Arrow Flight SQL, connectors
built by Redis versus third parties, full API / Query Engine access, and suggested
integration targets.

Apache Airflow has a runnable TaskFlow DAG and opt-in integration tests for
Arrow ingestion and SQL queries, plus checks of the official `AdbcHook` with
explicit autocommit. See [Airflow setup and limitations](docs/python-integrations.md#apache-airflow).

## Quick start

Requirements: Go 1.26.10 or newer, a C toolchain (cgo), Docker, and
[uv](https://docs.astral.sh/uv/). Run everything from the `go` directory.

The [v0.0.18 release](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.18)
has prebuilt libraries for macOS (Apple Silicon) and Linux (x86-64 and
arm64). The Linux libraries run on every Linux that
[Redis Software supports](https://redis.io/docs/latest/operate/rs/references/supported-platforms/):
RHEL 8 and 9 (and compatible distributions), Ubuntu 20.04 and later, and
Amazon Linux 2023. To use one instead of building, put it at the path
`make build` would create (`go/build/libadbc_driver_redis.dylib` or `.so`).
Releases after v0.0.10 also have the
[`redis-adbc`](docs/driver-reference.md#existing-hash-collections-redis-adbc) command for the same
platforms.

### 1. Start Redis Software and build the driver

```bash
cd go
make redis-up
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

-- Regular expressions and SIMILAR TO run in the driver, on the
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
docker compose exec redis-software /opt/redislabs/bin/redis-cli -p 12000 HGETALL public:sales:42
```

```bash
docker compose exec redis-software /opt/redislabs/bin/redis-cli -p 12000 FT.INFO idx:public:sales
```

To see the `FT.AGGREGATE` / `FT.CURSOR` commands the driver sends for each query,
run `MONITOR` in a second terminal while you run SQL:

```bash
docker compose exec redis-software /opt/redislabs/bin/redis-cli -p 12000 MONITOR
```

Stop and remove the disposable Redis Software database with `make redis-down` (from `go`).
