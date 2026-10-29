# ADBC support across 22 data engineering tools

Documentation checked on **2026-10-16**. This is a selection of prominent tools
across the data engineering stack, not a measured popularity ranking.
Ray and Dask documentation checked on **2026-10-20**.
dbt adapter documentation and linked CI evidence checked on **2026-10-21**.

**ADBC (Arrow Database Connectivity)** is a database interface designed to
exchange data in Apache Arrow's columnar format. Support has two distinct
directions: a tool can **consume an ADBC driver** to access another database,
or **have an ADBC driver** that lets other applications query the tool.
The latter does not imply that the tool can load the Redis driver.

## Tool comparison

**Redis ADBC support** means loading this repository's driver locally.
**Redis Arrow Flight SQL support** means connecting over the network to the
[Redis Arrow Flight server](https://github.com/alberttwong/redis-apache-arrow-flight#client-examples),
which runs the Redis ADBC driver on the server side. It requires a separate server
deployment; it is not a built-in endpoint of Redis. Its Flight datasets API is a
separate interface and does not establish Flight SQL compatibility.

Redis tool integrations below are **untested end to end in this repository**,
except the Ray Arrow-table bridge and Dask partitioned reader, which have recorded
local validations linked below. Ray's validation covers caller-side materialization
and subsequent Ray processing; it does not establish a worker-side Redis datasource.
The companion dbt adapter project records separate v1 and experimental v2
end-to-end validation; see [Testing status](#testing-status) for its scope.
"Candidate" describes an inferred connection path from documented APIs, not
confirmed compatibility. General Python ADBC validation and the related server's
Python/Go/JDBC coverage do not constitute tests of all 22 tools. See the
[Testing status](#testing-status).

The connector columns distinguish **Redis-built / maintained** projects from
**third-party-built** projects. Redis Field Engineering and the former RedisLabs
organization count as Redis provenance; Apache, Prefect, Airbyte, Fivetran,
CData, and independent extension authors do not. "None verified" is not a claim
that no such project exists. An official integration for a tool is not necessarily
built by Redis. The Redis ADBC driver and related Flight server are listed in
their own columns; the Redis name alone does not establish Redis, Inc. authorship
or support.

The **Redis connection solution** column summarizes qualifying connection paths:

- ✅ means a documented ADBC/Flight SQL integration path for Redis SQL/Arrow data,
  a Redis-built tool-specific data connector (including source/sink connectors),
  or a third-party integration providing Query Engine-backed SQL or native Redis
  command access. Driver installation, configuration, and compatibility testing
  may still be needed; candidate paths remain explicitly labeled in the table.
- ❌ means no qualifying path is established for the row, or a qualifying
  connector/adapter still needs development. Cache-only access is insufficient.
- A generic SDK such as **redis-py is not a tool connector**. Its availability in a
  Python runtime does not qualify a row for ✅ or fill either connector column.

The icons beside each green checkmark identify the qualifying path(s):

- 🔌 **Redis ADBC** — the tool can consume this driver's ADBC connection.
- ✈️ **Redis Arrow Flight SQL** — the tool can reach the Redis Flight SQL server
  through an ADBC Flight SQL or JDBC integration path.
- 🧩 **Redis-built connector** — a qualifying tool-specific connector built by
  Redis / Redis Field Engineering / RedisLabs.
- 🤝 **Third-party connector** — a qualifying tool-specific integration built by
  someone else, such as the Apache Airflow Redis provider or prefect-redis.

Multiple icons mean multiple paths contribute to the green status. An icon does
not establish an end-to-end Redis integration test; the adjacent columns retain
candidate, configuration, runtime, and validation limits. Spark's 🧩 refers to
spark-redis, and its ✈️ identifies the separate Flight SQL candidate. Kafka and
Flink qualify through their Redis-built source/sink connectors (✅ 🧩); their
connector columns describe supported data structures and operations. These badges
do not claim unrestricted command passthrough or Query Engine support where it
is not documented. Prefect's 🤝 refers to prefect-redis, not a Redis-built
connector or the generic redis-py SDK. Red rows keep ❌ without path icons because
none of their listed paths currently qualifies.

**Query Engine-backed SQL is a qualifying solution**, including Redis SQL Trino.
It does not need to expose arbitrary Redis commands to qualify through that SQL
path. ADBC/Flight SQL also expose supported SQL rather than unrestricted command
passthrough. For native-command connector claims, full Redis API access including
Query Engine commands must be established; cache-only access is insufficient.

Snowflake remains ❌ because its Openflow Redis cache service establishes neither
that command access nor Query Engine-backed SQL access. Trino is ✅ because
Redis-built **Redis SQL Trino** provides indexed SQL queries through the Query
Engine. Its README attributes copyright and support to Redis, Inc. The Trino
project's built-in Redis connector is a different, more limited connector.

Connector ownership and connection availability are separate: a third-party ADBC
integration can provide a solution even when no Redis-built connector is verified.
These symbols describe documented or explicitly labeled candidate paths, not
completed end-to-end tests or guarantees for every runtime.

| # | Tool | Redis connection solution | Main purpose | General ADBC support | Redis ADBC support | Redis Arrow Flight SQL support | Redis-built / maintained connector | Third-party-built connector / alternative |
|---|---|---|---|---|---|---|---|---|
| 1 | Apache Spark | ✅ ✈️ 🧩 | Distributed processing | **Driver available for querying Spark.** Its built-in external database reader uses JDBC; consuming another ADBC driver requires integration work. [Driver list](https://arrow.apache.org/adbc/main/driver/index.html), [JDBC reader](https://spark.apache.org/docs/latest/sql-data-sources-jdbc.html). | Custom Python or data-source integration; untested. | Candidate via Flight SQL JDBC and Spark JDBC reader; validate JVM setup, metadata, partitioning, and SQL. | [spark-redis](https://github.com/RedisLabs/spark-redis), RedisLabs: Redis data structures and Spark SQL; older documented Spark targets. Full Redis command / Query Engine coverage is not established for this connector. | None verified. |
| 2 | Databricks | ✅ ✈️ 🧩 | Lakehouse and processing | **Driver available** for ADBC clients to query Databricks. This does not establish a generic facility for loading the Redis driver into Databricks. [Driver list](https://arrow.apache.org/adbc/main/driver/index.html). | Custom notebook/Python integration; untested. Databricks ADBC is an outbound driver for querying Databricks. | Candidate via Spark JDBC on compute permitting custom drivers, or Python Flight SQL; untested. | [spark-redis](https://github.com/RedisLabs/spark-redis), RedisLabs: Spark library candidate, not a verified bundled Databricks connector. Check runtime compatibility; full Redis command / Query Engine coverage is not established for this connector. | None verified. |
| 3 | Snowflake | ❌ | Cloud warehouse | **Driver available** for accessing Snowflake from ADBC clients. [Driver list](https://arrow.apache.org/adbc/main/driver/index.html). | No generic Redis ADBC loading path verified inside the warehouse; an external pipeline can copy data. | No built-in Redis Flight SQL source verified; use an external ingestion pipeline. | None verified. | [Snowflake Openflow Redis cache service](https://docs.snowflake.com/en/user-guide/data-integration/openflow/controllers/redisdistributedmapcacheclientservice), Snowflake/Apache NiFi component. Cache-only access does not qualify; full Redis API and Query Engine commands are not established. |
| 4 | Google BigQuery | ❌ | Cloud warehouse | **Driver available** for accessing BigQuery from ADBC clients. [Driver list](https://arrow.apache.org/adbc/main/driver/index.html). | No generic Redis ADBC loading path verified inside BigQuery; an external pipeline can copy data. | No native Redis Flight SQL federation verified; use an external ingestion pipeline. | None verified. | No native Redis federation connector verified in [BigQuery supported federated sources](https://docs.cloud.google.com/bigquery/docs/federated-queries-intro). External replication is a separate pipeline. |
| 5 | Trino | ✅ 🧩 | Distributed SQL queries | **Driver available** for querying Trino; this is distinct from a Trino connector to an arbitrary ADBC source. [Driver list](https://arrow.apache.org/adbc/main/driver/index.html). | No generic Redis ADBC source connector verified; Trino would need connector work. | No built-in Redis Flight SQL connector verified; a custom Trino connector would be needed. | [Redis SQL Trino](https://github.com/redis-field-engineering/redis-sql-trino), built by Redis Field Engineering / Redis, Inc.: SQL over indexed HASH/JSON data with filters and aggregations pushed to the Query Engine. [Documentation](https://redis-field-engineering.github.io/redis-sql-trino/) explicitly describes FT.SEARCH / FT.AGGREGATE use and Redis Cluster configuration. This is separate from the Trino project's built-in Redis connector; unrestricted native command passthrough is not established. | [Built-in Trino Redis connector](https://trino.io/docs/current/connector/redis.html), Trino project: selected key/value reads, no Redis Cluster. This is a different connector from Redis SQL Trino. |
| 6 | Microsoft Fabric / Power Query | ❌ | Data preparation and analytics | **Yes, for supported connectors.** Power Query also documents custom ADBC connectors; availability depends on the host. [Connector development](https://learn.microsoft.com/en-us/power-query/adbc). | Custom Power Query connector required; untested with Redis. | Documented custom connector approach using ADBC Flight SQL; navigation, types, folding, gateway, and host support need validation. | None verified. | [CData Redis Power BI connector](https://www.cdata.com/kb/tech/redis-powerbi-setup.rst), CData: commercial ODBC-based Desktop/Gateway integration. Full Redis command and Query Engine passthrough not verified; not all Fabric hosts. |
| 7 | dbt | ✅ 🔌 | SQL transformations | **Yes, through adapters.** The Rust-based dbt v2 engine uses ADBC; dbt Core v1 can use it through a database-specific Python adapter. An ADBC driver alone does not register a database in either engine. [ADBC requirements](https://docs.getdbt.com/docs/dbt/adbc), [Adapter development](https://docs.getdbt.com/docs/contribute-dbt-adapters-v2?version=2). | Available: [dbt-redis-adbc for Core 1.10–1.x](https://github.com/alberttwong/redis-dbt-adapter-with-project/tree/main/v1/adapter) and an [experimental native v2 adapter](https://github.com/alberttwong/redis-dbt-adapter-with-project/tree/main/v2) compiled into the [custom engine fork](https://github.com/alberttwong/dbt). The standard v2 binary does not include Redis; install the native Redis driver separately. [Validation scope](https://github.com/alberttwong/redis-dbt-adapter-with-project/blob/main/v2/VALIDATION.md). | Unverified. The existing adapters load the Redis driver directly; they do not establish Flight SQL compatibility. Adapter connection/configuration work and end-to-end validation remain necessary. | None verified. | Community [v1/v2 adapter project](https://github.com/alberttwong/redis-dbt-adapter-with-project), with v2 Rust implementation in the engine fork. Redis, Inc. maintenance or support is not established. |
| 8 | Apache Airflow | ✅ 🔌 ✈️ 🤝 | Workflow orchestration | **Yes, through the Apache Arrow provider's `AdbcHook`.** Install the provider and database driver. [ADBC connection](https://airflow.apache.org/docs/apache-airflow-providers-apache-arrow/stable/connections/adbc.html). | Locally tested with Airflow 3.3.2 TaskFlow and Arrow provider 0.1.1: ingest, SQL aggregation and cleanup. AdbcHook requires explicit autocommit and driver discovery configuration; `get_records()` fails. [Setup and scope](python-integrations.md#apache-airflow). | Candidate through AdbcHook with the Flight SQL driver, or Python tasks; untested. | None verified. | [Apache Airflow Redis provider / RedisHook](https://airflow.apache.org/docs/apache-airflow-providers-redis/stable/_api/airflow/providers/redis/hooks/redis/index.html), Apache Airflow: get_conn() exposes a native Redis client for command calls. |
| 9 | Dagster | ✅ 🔌 ✈️ | Data orchestration | **Yes, through a community integration.** `dagster-adbc` provides `ADBCResource`. [Integration guide](https://docs.dagster.io/integrations/libraries/adbc). | Candidate through community ADBCResource and the Redis shared library; untested. | Candidate through ADBCResource with driver="flightsql"; untested. | None verified. | No dedicated Redis connector verified. Community dagster-adbc supplies ADBC integration; native command calls require redis-py in asset code. |
| 10 | Prefect | ✅ 🤝 | Python workflow orchestration | **Custom Python integration.** Tasks can call ADBC libraries; a dedicated ADBC connector was not verified. [Python task API](https://reference.prefect.io/prefect/tasks/). | Custom Python tasks using the Redis ADBC driver; untested. | Custom Python tasks using adbc_driver_flightsql; untested. | None verified. | [prefect-redis](https://docs.prefect.io/integrations/prefect-redis), Prefect: RedisDatabase, locks, and messaging. Its [RedisDatabase.get_client()](https://reference.prefect.io/prefect_redis/blocks/#prefect_redis.blocks.RedisDatabase.get_client) exposes a native Redis client for direct command calls; this tool-specific integration is distinct from installing redis-py alone. |
| 11 | Apache Kafka / Kafka Connect | ✅ 🧩 | Event streaming and integration | **No standard ADBC connector verified.** The common Confluent database connector uses JDBC. [JDBC connector](https://docs.confluent.io/kafka-connectors/jdbc/10.8/overview.html). | Custom Kafka Connect connector required; the JDBC connector cannot directly load an ADBC library. | Candidate through a JDBC source connector plus Flight SQL JDBC; verify polling/incremental SQL and type mappings. Not CDC. | [Redis Kafka Connector](https://redis-field-engineering.github.io/redis-kafka-connect/), Redis Field Engineering: sinks and keys/Streams sources; full command API / Query Engine access not established. | Confluent JDBC connector is a Flight SQL candidate, not a verified unrestricted Redis command connector. |
| 12 | Apache Flink | ✅ 🧩 | Stream processing | **No built-in generic ADBC connector verified.** Its documented relational database connector uses JDBC. [JDBC connector](https://nightlies.apache.org/flink/flink-docs-stable/docs/connectors/table/jdbc/). | Custom source/sink integration required; untested. | Candidate through the JDBC connector plus Flight SQL JDBC; validate dialect, types, batching, and streaming semantics. | [Redis Flink connector](https://github.com/redis-field-engineering/redis-flink-connector-dist), Redis Field Engineering / Redis, Inc.: Streams sources and Streams/JSON/Hash sinks. Full command API / Query Engine access not established. | Apache Flink JDBC connector is a Flight SQL candidate; check dialect and runtime compatibility. |
| 13 | Apache Beam | ❌ | Batch and streaming pipelines | **No built-in generic ADBC I/O verified.** Custom code is possible; documented database I/O includes JDBC. [JDBC I/O](https://beam.apache.org/releases/pydoc/current/apache_beam.io.jdbc.html). | Custom Beam I/O required; untested. | Candidate through Java JdbcIO / Python JDBC I/O with Flight SQL JDBC; runner packaging and query/write behavior need validation. | None verified. | [Java RedisIO](https://beam.apache.org/releases/javadoc/current/org/apache/beam/sdk/io/redis/RedisIO.html), Apache Beam: key/value reads/writes and Streams writes. Full command API / Query Engine access not established. |
| 14 | Apache NiFi | ❌ | Visual data flows | **No standard ADBC processor verified.** Its standard SQL processor uses JDBC. [ExecuteSQL](https://nifi.apache.org/components/org.apache.nifi.processors.standard.ExecuteSQL/). | Custom processor or script integration required; untested. | Candidate through JDBC processors and Flight SQL JDBC; validate JVM options, metadata, and result conversion. | None verified. | [Redis connection/cache services](https://nifi.apache.org/components/org.apache.nifi.redis.service.RedisDistributedMapCacheClientService/), Apache NiFi: Redis-backed caching. Full command API / Query Engine access not established. |
| 15 | Airbyte | ❌ | Data replication | **No generic ADBC connector verified.** An ADBC driver alone does not automatically become an Airbyte connector. [Connector catalog](https://docs.airbyte.com/integrations), [ADBC feature request](https://github.com/airbytehq/airbyte/issues/24546). | Custom Airbyte connector required; untested. | Custom connector required; no generic Flight SQL connector verified. | None verified. | [Marketplace Redis destination](https://docs.airbyte.com/integrations/destinations/redis), Airbyte ecosystem: data-structure writes. Redis source and full command API / Query Engine access not verified. |
| 16 | Fivetran | ❌ | Managed data replication | **Custom integration candidate.** Its Python Connector SDK can use database libraries, but ADBC packaging and runtime compatibility need validation. [SDK guide](https://fivetran.com/docs/connector-sdk/getting-started). | Custom Connector SDK source; binary packaging/runtime compatibility untested. | Custom Connector SDK source using the Python Flight SQL driver; untested. | None verified. | [Community Redis Connector SDK example](https://github.com/fivetran/community_connectors/tree/main/redis), Fivetran community: reads keyspace/TimeSeries data for replication. Does not expose unrestricted Redis API or Query Engine commands as a tool connector; custom SDK code could be extended. |
| 17 | dlt | ❌ | Python data loading | **Yes, for selected destinations.** Documents ADBC loading for MySQL, SQL Server, and SQLite; arbitrary drivers need integration work. [ADBC loading details](https://dlthub.com/blog/arrow-adbc-vs-sqlalchemy). | Custom resource can yield fetched Arrow batches; no packaged Redis source/destination verified. | Custom Python resource using Flight SQL can yield Arrow batches; untested. | None verified. | No packaged Redis keyspace connector verified. [dltHub Redis Labs REST API guide](https://dlthub.com/context/source/redis-labs) targets management APIs, not application rows or Query Engine commands. |
| 18 | pandas | ✅ 🔌 ✈️ | DataFrame processing | **Yes, directly.** SQL read APIs accept ADBC connections through the Python driver manager. [Integration guide](https://arrow.apache.org/adbc/main/integrations.html). | Documented ADBC connection API; pass the open Redis connection to read_sql. Redis integration untested. | Pass an open ADBC Flight SQL connection to read_sql; tool-level Redis integration untested. | None verified. | No dedicated Redis connector verified. Custom extraction can feed [pandas.DataFrame.from_records](https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.from_records.html). |
| 19 | Polars | ✅ 🔌 ✈️ | Fast DataFrame processing | **Yes, directly.** Database read/write APIs support ADBC connections. [Integration guide](https://arrow.apache.org/adbc/main/integrations.html). | **Locally validated, 2026-10-29:** connection/URI reads, typed write/append round trips, filtering and aggregation, NULLs, and empty-result schemas. The unchanged [Polars regression suite](../python/tests/test_polars.py) plus driver tests passed all 8 cases with warnings treated as errors on Redis Software 8.6.2 with verified 4x QPF. [Evidence and reproduction](sdlc/changes/polars-regression-rerun/validation.md). | Pass an open ADBC Flight SQL connection to read_database; tool-level Redis integration untested. | None verified. | No dedicated Redis connector verified. Custom extraction can feed [polars.from_dicts](https://docs.pola.rs/api/python/stable/reference/api/polars.from_dicts.html). |
| 20 | DuckDB | ✅ 🔌 ✈️ | Embedded analytical SQL | **Yes, in both directions.** DuckDB exposes an ADBC driver, and community extensions let it query other ADBC databases. [Integration guide](https://arrow.apache.org/adbc/main/integrations.html), [Community extension](https://duckdb.org/community_extensions/extensions/adbc). | Candidate through the community adbc extension and a Redis connection profile; untested. | Candidate through the adbc extension with Flight SQL, or fetch Arrow in Python and register it; untested. | None verified. | Experimental [redis extension](https://duckdb.org/community_extensions/extensions/redis), lmangani/gigapi: selected Redis functions, not verified full API / Query Engine access. The ADBC community extension is a separate project. |
| 21 | Ray / Ray Data | ❌ | Distributed Python and ML data processing | **Validated custom Arrow-table bridge; no native ADBC reader verified.** `read_sql` accepts a DB-API 2 connection factory; `from_arrow` imports PyArrow tables. Neither establishes native ADBC support. [SQL reader](https://docs.ray.io/en/latest/data/api/doc/ray.data.read_sql.html), [Arrow import](https://docs.ray.io/en/latest/data/api/doc/ray.data.from_arrow.html). | [Locally validated](sdlc/changes/ray-356/validation.md): fetch a table through Redis ADBC, close the connection, then pass it to from_arrow. Ray 2.59.0 preserved rows, NULLs, Arrow types and empty-result schemas, including subsequent worker processing. The complete query result is materialized in the caller. Direct read_sql use remains untested. | Custom Python loading with adbc_driver_flightsql and from_arrow; untested. The Redis ADBC bridge validation does not establish Flight SQL compatibility. | None verified. | No packaged Ray Redis datasource verified. The tested bridge is custom loading code; worker-side datasource work remains tracked in [#164](https://github.com/redis/redis-adbc-driver/issues/164). |
| 22 | Dask | ✅ 🔌 | Parallel DataFrames and Python computation | **Via this repository's Redis adapter; no built-in generic ADBC reader verified.** Dask SQL readers use SQLAlchemy URIs; custom readers can use from_map. [SQL integration](https://docs.dask.org/en/stable/dataframe-sql.html), [Custom loading](https://docs.dask.org/en/latest/generated/dask.dataframe.from_map.html). | [adbc_driver_redis.dask.read_query](python-integrations.md#dask) provides lazy partitioned reads with worker-local ADBC connections and pandas Arrow dtypes. [Recorded local validation](sdlc/changes/dask-353/validation.md) includes two worker processes; caller supplies partition parameters and exact schema. | Custom from_map loading with adbc_driver_flightsql and pandas partitions; untested. The bundled Redis helper does not establish Flight SQL compatibility. | None verified. | This repository's [Dask adapter](../python/adbc_driver_redis/dask.py) supplies the ADBC path; Redis, Inc. authorship or support is not established. Native Redis command access is not claimed. |

"No support verified" means the checked documentation did not establish generic
support; it does not rule out custom or third-party integrations. JDBC connector
documentation establishes a documented connection path, not proof that no ADBC
integration exists. Arrow support alone does not imply ADBC support. Some linked
pages track development or latest versions, so check the installed release.

## Custom SDK code is not a connector

[redis-py](https://github.com/redis/redis-py) is a Redis client SDK. Users can write
custom tasks, notebooks, or resources with it, including calls to Query Engine
commands, but that work is not a pre-existing tool-specific connector. It is
excluded from both connector inventory columns and is not a solution indicator
on its own. ADBC and Flight SQL paths are evaluated separately.

## Implications for the Redis ADBC driver

Suggested first integration targets are **pandas, Polars, Airflow, Dagster,
and DuckDB** because they have documented paths for consuming ADBC drivers.
This is a prioritization recommendation, not a claim of tested Redis compatibility.

Fabric / Power Query is another candidate with custom connector work. dbt has
community Redis adapters for Core 1.10–1.x and experimental v2 support in a custom
engine fork. The v1 Python package does not enable Redis in the standard v2
executable. Both documented paths load the native Redis ADBC driver directly;
Flight SQL remains unverified.
Selected ADBC destinations in dlt do not establish an arbitrary Redis destination.
Fivetran would require a custom connector and validation of driver deployment.

Ray's Arrow-table bridge is [locally validated](sdlc/changes/ray-356/validation.md),
but requires custom loading code and materializes the full query result in the
caller. It remains ❌ under this comparison's tool-specific connector criteria:
a packaged worker-side Redis datasource is still pending in
[#164](https://github.com/redis/redis-adbc-driver/issues/164). Its DB-API reader
and Flight SQL path are separate, untested compatibility candidates.

Dask is ✅ 🔌 through
this repository's partitioned read adapter. Its caller must supply disjoint,
exhaustive partition predicates and an exact Arrow schema; each partition is
materialized in worker memory, and tasks do not share a snapshot. Install the
package and native driver on every worker. See the
[Dask partitioning contract](python-integrations.md#dask).

## Testing status

The [Python ADBC validation suite](../go/validation/tests/) exercises the Redis
shared library, including queries, metadata, ingestion, errors, and cancellation.
PyArrow is used by the [row-value tests](../go/validation/tests/test_row_values.py)
and [sample loader](../go/examples/load_sample.py). See the README's
[Compatibility and validation](../README.md#compatibility-and-validation) and
[Python usage](driver-reference.md#python-usage) for recorded results and connection setup.
This coverage does not establish end-to-end compatibility with every tool above.

Ray's [bridge validation record](sdlc/changes/ray-356/validation.md) and
[regression tests](../python/tests/test_ray_arrow.py) cover Redis ADBC → Arrow
table → Ray Data with Ray 2.59.0, including SELECT 1, nullable scalar types,
empty-result schemas and a normal worker-side map_batches operation. All three
tests passed locally on macOS arm64 against standalone Redis 8.6.2. This is a
source-build, small-data bridge test, not distributed Redis ingestion, a
multi-node Ray test or evidence for every release wheel or deployment.

Dask has a separate [local validation record](sdlc/changes/dask-353/validation.md)
for partitioned Redis ADBC reads, including two worker processes, range and NULL
partitions, empty results, Arrow dtypes, and connection cleanup. This is recorded
source-install validation, not evidence for every deployment or Flight SQL.

The companion [dbt adapter project](https://github.com/alberttwong/redis-dbt-adapter-with-project)
records separate end-to-end validation. Its [v1 guide](https://github.com/alberttwong/redis-dbt-adapter-with-project/blob/main/v1/README.md#validation-and-testing)
describes full dbt workflows and the dbt adapter functional suite for Core
1.10–1.x. The [v2 validation record](https://github.com/alberttwong/redis-dbt-adapter-with-project/blob/main/v2/VALIDATION.md)
reports macOS arm64 and Linux coverage with dbt-oss 2.0.8, Redis 8.6.2 and Redis
ADBC v0.0.16, using custom engine commit
`90178580bd6f3353d758fff770fdc2c58c985ad0`. The linked
[Linux CI run](https://github.com/alberttwong/redis-dbt-adapter-with-project/actions/runs/39623533131)
completed successfully on October 21, 2026. Its recorded coverage includes smoke
and feature fixtures, trusted TLS, and the full taxi workflow: 82 successful
build results plus one configured source warning, and 82 successes for the empty
build. Python table models, snapshots, contracts, merge, microbatch and persisted
docs are included in the recorded scenarios. This is experimental custom-engine
support; the standard dbt v2 executable does not include Redis, static analysis
remains disabled, and these results do not establish Flight SQL compatibility or
support for every deployment. SQL grants and materialized views remain unsupported
in both adapters.

The [differential harness](../go/pgdiff/README.md) replays captured dbt-postgres SQL
through Redis ADBC. It does not validate a Redis dbt adapter or a full dbt run,
snapshots, or documentation generation against Redis.

The related [Redis Arrow Flight server](https://github.com/alberttwong/redis-apache-arrow-flight#development)
documents Python ADBC, Go, and JDBC client coverage. Those results do not establish
tests of each application against this driver's current checkout. Application
configuration, metadata discovery, generated SQL, type mappings, authentication,
and refresh behavior still require validation where applicable.

No integration suites were rerun for this documentation update; the Ray, Dask and
dbt claims cite their recorded test runs above. SQL syntax similarity alone
does not establish JDBC, ODBC, SQLAlchemy, or PostgreSQL wire-protocol compatibility.
