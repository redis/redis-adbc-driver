# ADBC support across 20 data engineering tools

Documentation checked on **2026-10-16**. This is a selection of prominent tools
across the data engineering stack, not a measured popularity ranking.

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

All Redis tool integrations below are **untested end to end in this repository**.
"Candidate" describes an inferred connection path from documented APIs, not
confirmed compatibility. General Python ADBC validation and the related server's
Python/Go/JDBC coverage do not constitute tests of all 20 tools. See the
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
| 7 | dbt | ❌ | SQL transformations | **Yes, in the newer Rust-based engine, documented as dbt v2.** A new database also needs an adapter, SQL support, and metadata implementation; this does not imply generic ADBC support in dbt v1. [ADBC requirements](https://docs.getdbt.com/docs/dbt/adbc), [Adapter development](https://docs.getdbt.com/docs/contribute-dbt-adapters-v2?version=2). | Redis adapter still required; captured dbt SQL replay is not a full dbt integration. | Custom dbt adapter and driver configuration still required; a Flight SQL endpoint alone is insufficient. | None verified. | No Redis adapter verified in [dbt supported platforms](https://docs.getdbt.com/docs/supported-data-platforms). |
| 8 | Apache Airflow | ✅ 🔌 ✈️ 🤝 | Workflow orchestration | **Yes, through the Apache Arrow provider's `AdbcHook`.** Install the provider and database driver. [ADBC connection](https://airflow.apache.org/docs/apache-airflow-providers-apache-arrow/stable/connections/adbc.html). | Candidate through AdbcHook plus a Redis driver package/entry point; configure autocommit and validate. | Candidate through AdbcHook with the Flight SQL driver, or Python tasks; untested. | None verified. | [Apache Airflow Redis provider / RedisHook](https://airflow.apache.org/docs/apache-airflow-providers-redis/stable/_api/airflow/providers/redis/hooks/redis/index.html), Apache Airflow: get_conn() exposes a native Redis client for command calls. |
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
| 19 | Polars | ✅ 🔌 ✈️ | Fast DataFrame processing | **Yes, directly.** Database read/write APIs support ADBC connections. [Integration guide](https://arrow.apache.org/adbc/main/integrations.html). | Documented ADBC connection API; pass the open Redis connection to read_database. Redis integration untested. | Pass an open ADBC Flight SQL connection to read_database; tool-level Redis integration untested. | None verified. | No dedicated Redis connector verified. Custom extraction can feed [polars.from_dicts](https://docs.pola.rs/api/python/stable/reference/api/polars.from_dicts.html). |
| 20 | DuckDB | ✅ 🔌 ✈️ | Embedded analytical SQL | **Yes, in both directions.** DuckDB exposes an ADBC driver, and community extensions let it query other ADBC databases. [Integration guide](https://arrow.apache.org/adbc/main/integrations.html), [Community extension](https://duckdb.org/community_extensions/extensions/adbc). | Candidate through the community adbc extension and a Redis connection profile; untested. | Candidate through the adbc extension with Flight SQL, or fetch Arrow in Python and register it; untested. | None verified. | Experimental [redis extension](https://duckdb.org/community_extensions/extensions/redis), lmangani/gigapi: selected Redis functions, not verified full API / Query Engine access. The ADBC community extension is a separate project. |

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

Fabric / Power Query is another candidate with custom connector work. dbt requires
a larger investment because the driver is only one part of database compatibility.
Selected ADBC destinations in dlt do not establish an arbitrary Redis destination.
Fivetran would require a custom connector and validation of driver deployment.

## Testing status

The [Python ADBC validation suite](../go/validation/tests/) exercises the Redis
shared library, including queries, metadata, ingestion, errors, and cancellation.
PyArrow is used by the [row-value tests](../go/validation/tests/test_row_values.py)
and [sample loader](../go/examples/load_sample.py). See the README's
[Compatibility and validation](../README.md#compatibility-and-validation) and
[Python usage](../README.md#python-usage) for recorded results and connection setup.
This coverage does not establish end-to-end compatibility with every tool above.

The [differential harness](../go/pgdiff/README.md) replays captured dbt-postgres SQL
through Redis ADBC. It does not validate a Redis dbt adapter or a full dbt run,
snapshots, or documentation generation against Redis.

The related [Redis Arrow Flight server](https://github.com/alberttwong/redis-apache-arrow-flight#development)
documents Python ADBC, Go, and JDBC client coverage. Those results do not establish
tests of each application against this driver's current checkout. Application
configuration, metadata discovery, generated SQL, type mappings, authentication,
and refresh behavior still require validation where applicable.

No new integration tests were run for this comparison. SQL syntax similarity alone
does not establish JDBC, ODBC, SQLAlchemy, or PostgreSQL wire-protocol compatibility.
