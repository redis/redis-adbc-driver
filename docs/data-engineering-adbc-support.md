# ADBC support across 20 data engineering tools

Support and testing status refreshed on **2026-10-29** against this repository
and selected upstream ADBC documentation. The broader connector inventory retains
the **2026-10-16** review baseline; this refresh is not a new audit of every
third-party connector. This is a selection of prominent tools across the data
engineering stack, not a measured popularity ranking.

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

**Recorded Redis integration tests now cover Polars, the repository's Dagster
IO manager, and two DuckDB paths.** Other paths remain candidates unless
explicitly stated. "Candidate" describes an inferred connection path from
documented APIs, not confirmed compatibility. A passing test applies to its
recorded versions, environment, and operations; it does not validate every API
or deployment. General ADBC validation and the related Flight server's client
coverage do not constitute tests of all 20 tools. See the
[Testing status](#testing-status) for dated evidence and remaining gaps.

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
| 8 | Apache Airflow | ✅ 🔌 ✈️ 🤝 | Workflow orchestration | **Yes, through the Apache Arrow provider's `AdbcHook`.** Install the provider and database driver. [ADBC connection](https://airflow.apache.org/docs/apache-airflow-providers-apache-arrow/stable/connections/adbc.html). | Candidate through AdbcHook with the published `adbc_driver_redis` package; configure `autocommit=True` and validate package discovery, URI handling, and hook operations. No Airflow integration test recorded. | Candidate through AdbcHook with the Flight SQL driver, or Python tasks; untested. | None verified. | [Apache Airflow Redis provider / RedisHook](https://airflow.apache.org/docs/apache-airflow-providers-redis/stable/_api/airflow/providers/redis/hooks/redis/index.html), Apache Airflow: get_conn() exposes a native Redis client for command calls. |
| 9 | Dagster | ✅ 🔌 ✈️ | Data orchestration | **Yes, through a community integration.** `dagster-adbc` provides `ADBCResource`. [Integration guide](https://docs.dagster.io/integrations/libraries/adbc). | **Tested repository integration:** `adbc_driver_redis.dagster.RedisADBCIOManager` supports Arrow, pandas, Polars, and reader inputs/outputs, including partition tests. Install the `dagster` extra; [guide](python-integrations.md#dagster). Community ADBCResource remains a separate, untested path. | Candidate through ADBCResource with driver="flightsql"; untested. | None verified. | Community dagster-adbc supplies generic ADBC integration. This repository's Redis-specific IO manager is listed in the Redis ADBC column; its presence does not establish Redis, Inc. maintenance. No separate native-command connector verified. |
| 10 | Prefect | ✅ 🤝 | Python workflow orchestration | **Custom Python integration.** Tasks can call ADBC libraries; a dedicated ADBC connector was not verified. [Python task API](https://reference.prefect.io/prefect/tasks/). | Custom Python tasks using the Redis ADBC driver; untested. | Custom Python tasks using adbc_driver_flightsql; untested. | None verified. | [prefect-redis](https://docs.prefect.io/integrations/prefect-redis), Prefect: RedisDatabase, locks, and messaging. Its [RedisDatabase.get_client()](https://reference.prefect.io/prefect_redis/blocks/#prefect_redis.blocks.RedisDatabase.get_client) exposes a native Redis client for direct command calls; this tool-specific integration is distinct from installing redis-py alone. |
| 11 | Apache Kafka / Kafka Connect | ✅ 🧩 | Event streaming and integration | **No standard ADBC connector verified.** The common Confluent database connector uses JDBC. [JDBC connector](https://docs.confluent.io/kafka-connectors/jdbc/10.8/overview.html). | Custom Kafka Connect connector required; the JDBC connector cannot directly load an ADBC library. | Candidate through a JDBC source connector plus Flight SQL JDBC; verify polling/incremental SQL and type mappings. Not CDC. | [Redis Kafka Connector](https://redis-field-engineering.github.io/redis-kafka-connect/), Redis Field Engineering: sinks and keys/Streams sources; full command API / Query Engine access not established. | Confluent JDBC connector is a Flight SQL candidate, not a verified unrestricted Redis command connector. |
| 12 | Apache Flink | ✅ 🧩 | Stream processing | **No built-in generic ADBC connector verified.** Its documented relational database connector uses JDBC. [JDBC connector](https://nightlies.apache.org/flink/flink-docs-stable/docs/connectors/table/jdbc/). | Custom source/sink integration required; untested. | Candidate through the JDBC connector plus Flight SQL JDBC; validate dialect, types, batching, and streaming semantics. | [Redis Flink connector](https://github.com/redis-field-engineering/redis-flink-connector-dist), Redis Field Engineering / Redis, Inc.: Streams sources and Streams/JSON/Hash sinks. Full command API / Query Engine access not established. | Apache Flink JDBC connector is a Flight SQL candidate; check dialect and runtime compatibility. |
| 13 | Apache Beam | ❌ | Batch and streaming pipelines | **No built-in generic ADBC I/O verified.** Custom code is possible; documented database I/O includes JDBC. [JDBC I/O](https://beam.apache.org/releases/pydoc/current/apache_beam.io.jdbc.html). | Custom Beam I/O required; untested. | Candidate through Java JdbcIO / Python JDBC I/O with Flight SQL JDBC; runner packaging and query/write behavior need validation. | None verified. | [Java RedisIO](https://beam.apache.org/releases/javadoc/current/org/apache/beam/sdk/io/redis/RedisIO.html), Apache Beam: key/value reads/writes and Streams writes. Full command API / Query Engine access not established. |
| 14 | Apache NiFi | ❌ | Visual data flows | **No standard ADBC processor verified.** Its standard SQL processor uses JDBC. [ExecuteSQL](https://nifi.apache.org/components/org.apache.nifi.processors.standard.ExecuteSQL/). | Custom processor or script integration required; untested. | Candidate through JDBC processors and Flight SQL JDBC; validate JVM options, metadata, and result conversion. | None verified. | [Redis connection/cache services](https://nifi.apache.org/components/org.apache.nifi.redis.service.RedisDistributedMapCacheClientService/), Apache NiFi: Redis-backed caching. Full command API / Query Engine access not established. |
| 15 | Airbyte | ❌ | Data replication | **No generic ADBC connector verified.** An ADBC driver alone does not automatically become an Airbyte connector. [Connector catalog](https://docs.airbyte.com/integrations), [ADBC feature request](https://github.com/airbytehq/airbyte/issues/24546). | Custom Airbyte connector required; untested. | Custom connector required; no generic Flight SQL connector verified. | None verified. | [Marketplace Redis destination](https://docs.airbyte.com/integrations/destinations/redis), Airbyte ecosystem: data-structure writes. Redis source and full command API / Query Engine access not verified. |
| 16 | Fivetran | ❌ | Managed data replication | **Custom integration candidate.** Its Python Connector SDK can use database libraries, but ADBC packaging and runtime compatibility need validation. [SDK guide](https://fivetran.com/docs/connector-sdk/getting-started). | Custom Connector SDK source; binary packaging/runtime compatibility untested. | Custom Connector SDK source using the Python Flight SQL driver; untested. | None verified. | [Community Redis Connector SDK example](https://github.com/fivetran/community_connectors/tree/main/redis), Fivetran community: reads keyspace/TimeSeries data for replication. Does not expose unrestricted Redis API or Query Engine commands as a tool connector; custom SDK code could be extended. |
| 17 | dlt | ❌ | Python data loading | **Yes, for selected destinations.** Documents ADBC loading for MySQL, SQL Server, and SQLite; arbitrary drivers need integration work. [ADBC loading details](https://dlthub.com/blog/arrow-adbc-vs-sqlalchemy). | Custom resource can yield fetched Arrow batches; no packaged Redis source/destination verified. | Custom Python resource using Flight SQL can yield Arrow batches; untested. | None verified. | No packaged Redis keyspace connector verified. [dltHub Redis Labs REST API guide](https://dlthub.com/context/source/redis-labs) targets management APIs, not application rows or Query Engine commands. |
| 18 | pandas | ✅ 🔌 ✈️ | DataFrame processing | **Yes, directly.** SQL read APIs accept ADBC connections through the Python driver manager. [read_sql](https://pandas.pydata.org/docs/reference/api/pandas.read_sql.html). | Candidate: pass an open Redis ADBC DB-API connection to `read_sql`. Direct pandas SQL reads remain untested here; pandas DataFrame round trips through the Dagster IO manager are recorded separately. | Pass an open ADBC Flight SQL connection to read_sql; tool-level Redis integration untested. | None verified. | No dedicated Redis connector verified. Custom extraction can feed [pandas.DataFrame.from_records](https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.from_records.html). |
| 19 | Polars | ✅ 🔌 ✈️ | Fast DataFrame processing | **Yes, directly.** Database read/write APIs support ADBC connections; URI reads support `engine="adbc"`. [URI read API](https://docs.pola.rs/api/python/stable/reference/api/polars.read_database_uri.html). | **Tested:** `read_database_uri(query, "redis://...", engine="adbc")` discovers `adbc_driver_redis`; DB-API/URI smoke tests and a clean-wheel run are recorded. `read_database` with an open connection is another documented API, not separately validated by that smoke test. [Guide](python-integrations.md#install-and-discover-the-driver). | Pass an open ADBC Flight SQL connection to read_database; tool-level Redis integration untested. | None verified. | No dedicated Redis connector verified. Custom extraction can feed [polars.from_dicts](https://docs.pola.rs/api/python/stable/reference/api/polars.from_dicts.html). |
| 20 | DuckDB | ✅ 🔌 ✈️ | Embedded analytical SQL | **Yes, in both directions.** DuckDB exposes an ADBC driver, and distinct community extensions consume drivers: [Query.Farm adbc_scanner](https://query.farm/products/extensions/adbc_scanner/) and [Columnar adbc](https://duckdb.org/community_extensions/extensions/adbc). | **Tested:** Query.Farm `adbc_scanner` attachment, reads/joins, ingestion, and autocommit writes; [guide](duckdb.md). Also tested: this repository's standalone [`redis_adbc`](../duckdb-extension/README.md), which embeds the driver and streams SELECT results. The repository documents a source build while registry acceptance is pending. Columnar's separate `adbc` remains untested here. | Candidate through a generic ADBC extension with Flight SQL, or fetch Arrow in Python and register it; untested. The standalone `redis_adbc` extension connects directly to Redis and does not consume Flight SQL. | None verified. | Experimental [redis extension](https://duckdb.org/community_extensions/extensions/redis), lmangani/gigapi: selected Redis functions, not verified full API / Query Engine access. Generic `adbc_scanner` and `adbc` are separate projects; Redis test coverage is specific to the extension named in the Redis ADBC column. |

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

Start with the implemented **Polars, Dagster, and DuckDB** paths in the table.
The [Python integration guide](python-integrations.md) documents the published
[`adbc-driver-redis`](https://pypi.org/project/adbc-driver-redis/) package,
DB-API connection setup, TLS alias, and native driver manifests. Wheels bundle
the driver for Python 3.10+ on Linux amd64/arm64 (glibc 2.28+) and macOS arm64
(macOS 12+). Non-Python driver consumers still need a manifest or library path.
Redis with Search must run separately; see [server requirements](../README.md#server-requirements).
Writes autocommit, and the driver does not provide transaction rollback or isolation.

**Airflow and direct pandas SQL reads** are useful next validation targets:
their ADBC APIs are documented, but this repository has no recorded end-to-end
Redis tests for those paths. Fabric / Power Query needs custom connector work.
dbt requires an adapter as well as driver and SQL compatibility. Selected ADBC
destinations in dlt do not establish an arbitrary Redis destination. Fivetran
requires a custom connector and validation of driver deployment.

The package also ships [SQLAlchemy and Superset integrations](python-integrations.md#sqlalchemy-and-superset),
outside this 20-tool selection. Their recorded checks cover reflection, bound SQL,
Superset discovery, dataset queries, and the SQL Lab cursor path; they do not
establish browser-rendered chart coverage or transactional ORM support.

## Testing status

These are **historical recorded results**, not fresh integration runs performed
for this documentation update. The [2026-10-20 Python verification record](sdlc/changes/271-280/validation.md)
used macOS arm64, Python 3.12, and disposable Redis 8.6.2 with Search. It records
ADBC manager 1.12.0, Polars 2.0.0, pandas 3.0.6, Dagster 1.13.26, and DuckDB 1.5.6.

| Path | Recorded evidence | Limits and remaining gaps |
|---|---|---|
| DB-API and Polars URI reads | [Python suite](../python/tests/test_driver.py): part of **17 passed, 1 skipped**; clean-wheel driver smoke run: **4 passed**. | Arbitrary Polars queries/types, direct Polars write APIs, and every wheel/platform combination. TLS alias discovery is not a TLS handshake test. |
| Dagster IO manager | [Tests](../python/tests/test_dagster.py): Arrow, pandas, Polars, readers, materializations, partition retries, and an injected stream failure in the same Python suite. | Community `dagster-adbc` / `ADBCResource` is untested here. Multi-dimensional partitions and atomic/transactional replacement are unsupported. |
| DuckDB via Query.Farm `adbc_scanner` | [Test](../python/tests/test_duckdb.py) and [verification record](sdlc/changes/duckdb-community/validation.md): reads, joins, ingestion, direct INSERT/CTAS, read-only enforcement, and transaction rejection. | Columnar's `adbc` extension, Flight SQL, older extension builds, and universal predicate pushdown. |
| Standalone DuckDB `redis_adbc` | [Native tests](../duckdb-extension/test/test_native.py): **16 passed** with DuckDB 1.5.6; matching CLI harness: **12 scenarios** on both 1.5.6 and the pinned DuckDB 2.0 registry source, checking 10,003 rows. [Record](sdlc/changes/duckdb-community/validation.md). | Registry availability and all build targets remain unverified. Writes, attached catalogs, and bound query parameters are unsupported; this extension is a SELECT-only interface. |
| SQLAlchemy / Superset (outside the table) | Separate Superset environment: **3 passed**, including SQLAlchemy reflection and Superset dataset / SQL Lab paths. [Tests](../python/tests/test_superset.py). | Browser-rendered charts, broad application workflows, and transaction semantics beyond autocommit. |

The Python suite's skipped Superset test ran separately in its own environment.
Configured CI jobs are not evidence that every job has passed; consult the linked
records for platform limits and the workflows for reproducible commands. Historical
passes do not certify the current checkout or latest upstream releases.

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

No new integration tests were run for this refresh. SQL syntax similarity alone
does not establish JDBC, ODBC, or PostgreSQL wire-protocol compatibility, or
SQLAlchemy compatibility beyond the explicitly tested dialect operations.
