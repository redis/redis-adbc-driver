# ADBC support across 50 popular data engineering and data science tools

The standard way to integrate your application with Redis is to use the
[official Redis clients and libraries](https://redis.io/docs/latest/develop/clients/)
for your programming language. Redis ADBC is another option for workflows that
benefit from SQL and Apache Arrow, including the data-tool integrations described here.

For installation and diagnostics, start with [Choose your tool and connect](setup.md).
See [installation choices for every integration](integration-installation.md) for
package routes, worker placement and release automation.


Support and testing status refreshed on **2026-11-03** against this repository
and selected upstream ADBC documentation. The broader connector inventory retains
the **2026-10-16** review baseline; this refresh is not a new audit of every
third-party connector.

The comparison focuses on tools that process, orchestrate, analyze, validate,
or present data. It excludes warehouses and products whose primary role is
storing data. The list is ordered roughly by broad adoption, informed by the
[2025 Apache Airflow survey](https://airflow.apache.org/blog/airflow-survey-2025/)
and the [2024 Python Developers Survey](https://lp.jetbrains.com/python-developers-survey-2024/), alongside ecosystem reach. These sources cover
different communities and do not provide one comparable score for all 50 tools, so this is an editorial popularity-informed
ranking rather than a precise universal leaderboard.
Dask documentation checked on **2026-10-20**. Ray worker reader checked on **2026-10-29**.
dbt adapter documentation and linked CI evidence checked on **2026-10-21**.
dbt repository location verified on **2026-11-01**: [redis/redis-dbt-adapter](https://github.com/redis/redis-dbt-adapter).
Airbyte source implementation and upstream draft status checked on **2026-11-01**;
see [Airbyte Redis ADBC source](#airbyte-redis-adbc-source).

**ADBC (Arrow Database Connectivity)** is a database interface designed to
exchange data in Apache Arrow's columnar format. Support has two distinct
directions: a tool can **consume an ADBC driver** to access another database,
or **have an ADBC driver** that lets other applications query the tool.
The latter does not imply that the tool can load the Redis driver.

## Tool comparison

**Redis ADBC support** means loading this repository's driver locally.
**Redis Arrow Flight SQL support** means connecting over the network to the
[Redis Arrow Flight server](https://github.com/redis/redis-apache-arrow-flight#client-examples),
which runs the Redis ADBC driver on the server side. It requires a separate server
deployment; it is not a built-in endpoint of Redis. Its Flight datasets API is a
separate interface and does not establish Flight SQL compatibility.

Recorded Redis integration coverage includes direct pandas SQL reads/writes, Polars,
Dagster's community ADBCResource and this repository's IO manager, two DuckDB
paths, Airflow TaskFlow and the packaged RedisAdbcHook, Prefect custom Python
tasks, Ray worker-side reads and
the Arrow-table bridge, and
the Dask partitioned reader. The repository also ships an append-only dlt
destination with recorded live validation on earlier sources and partial public-base
validation; see its evidence below. The companion dbt adapter project records separate
v1 and experimental v2 end-to-end validation. See [Testing status](#testing-status)
for dated evidence and limits. Ray's worker reader streams batches in local
worker-process tests; multi-node operation remains unverified. The older bridge
materializes the full result in the caller and has separate validation.

"Candidate" describes an inferred connection path from documented APIs, not
confirmed compatibility. A passing test applies to its recorded versions,
environment, and operations. General ADBC validation and the related Flight
server's client coverage do not constitute tests of all 50 tools.

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
spark-redis, and its ✈️ identifies the separately tested Flight SQL JDBC reader. Kafka and
Flink qualify through their Redis-built source/sink connectors (✅ 🧩); their
connector columns describe supported data structures and operations. These badges
do not claim unrestricted command passthrough or Query Engine support where it
is not documented. Prefect's 🤝 refers to prefect-redis, not a Redis-built
connector or the generic redis-py SDK. Its 🔌 now refers to the validated custom
Python ADBC task path, not a packaged Prefect ADBC connector. Red rows keep ❌ without path icons because
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

The **Redis read/write** column describes data movement relative to Redis:

- 📖 **Read** — fetch data from Redis into the tool.
- ✍️ **Write** — send data from the tool into Redis.
- 📖 ✍️ **Both** — the listed integration provides both directions.
- ⚠️ **Unverified** — no implemented Redis path establishes either direction.

Icons summarize the documented or exercised path, not every capability that
custom code could add. A lone 📖 or ✍️ means only that direction is established
for that path; it does not prove the underlying driver cannot do the reverse.
Saving model files, charts, or datasets outside Redis does not count as a Redis
write. Test-fixture setup alone does not establish a tool's write integration.
The adjacent support columns retain validation and runtime limits; no Flight SQL
write support is inferred from a read example. Cache-only or otherwise excluded
connectors can have a direction while retaining ❌ in the solution column.

**Read/write examples updated 2026-11-01:** all 34 green 🔌 rows have a
bidirectional guide path; Spark's custom bridge and DuckDB also passed, for 36
guides total. [Verification](sdlc/changes/integration-read-write/validation.md)
records actual versions and results. **Custom Arrow output** means the tool's
computed rows, predictions, chart data or summaries are ingested through
application code and checked on a fresh connection. These finite examples fully
materialize output and do not supply native distributed sinks, Redis model or
artifact stores, remote-runtime validation, transactions or Flight SQL writes.
Historical test counts below retain their original scope.

| # | Tool | Redis connection solution | Redis read/write | Main purpose | General ADBC support | Redis ADBC support | Redis Arrow Flight SQL support | Redis-built / maintained connector | Third-party-built connector / alternative |
|---|---|---|---|---|---|---|---|---|---|
| 1 | pandas | ✅ 🔌 ✈️ | 📖 ✍️ ADBC | DataFrame processing | **Yes, directly.** SQL read APIs accept ADBC connections through the Python driver manager. [Integration guide](https://arrow.apache.org/adbc/main/integrations.html). | Tested `read_sql` / `read_sql_query` and `DataFrame.to_sql` with an open Redis ADBC connection: nullable typed round trips, append, replacement, filtering/aggregation, empty reads, and multiple Arrow batches. Combined read/write suite: **12 passed**. [Evidence](sdlc/changes/pandas-smoke/validation.md). | Tested 2026-11-03: Tested `pd.read_sql` on an autocommit ADBC Flight SQL connection: ordered stored rows, typed values, NULLs and Unicode. No new Flight SQL write or `params=` claim. [Flight evidence][flight-core]. | None verified. | No dedicated Redis connector verified. Custom extraction can feed [pandas.DataFrame.from_records](https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.from_records.html). |
| 2 | NumPy | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/numpy.md) | Numerical computing | ADBC results can be converted to Arrow and NumPy arrays through Python clients; NumPy itself is not a database connector. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested a Redis ADBC Arrow query result converted to a NumPy array and checked numeric values; custom application code only. | Tested 2026-11-03: Tested Flight SQL Arrow results converted to NumPy arrays, including numeric values and a NULL-to-NaN value; custom application pipeline. [Flight evidence][flight-core]. | None verified. | No dedicated Redis connector; custom application code required. |
| 3 | Apache Spark | ✅ ✈️ 🧩 | 📖 ✍️ spark-redis; [custom ADBC bridge/output](integrations/spark.md) | Distributed processing | **Driver available for querying Spark.** Its built-in external database reader uses JDBC; consuming another ADBC driver requires integration work. [Driver list](https://arrow.apache.org/adbc/main/driver/index.html), [JDBC reader](https://spark.apache.org/docs/latest/sql-data-sources-jdbc.html). | Tested a custom Python path: native Redis ADBC fetch → Arrow → local Spark 4.3.0; typed rows, NULLs/Unicode, filters, SQL aggregation, and empty schemas. This is not a Spark database datasource. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested Spark 4.3.0 JDBC datasource with Flight SQL JDBC 19.0.0 and JDK 21.0.13: typed rows, NULLs/Unicode, filter, aggregate, two predicate partitions and empty result. Local execution; distributed deployment and writes untested. [Flight evidence][flight-spark]. | [spark-redis](https://github.com/RedisLabs/spark-redis), RedisLabs: Redis data structures and Spark SQL; older documented Spark targets. Full Redis command / Query Engine coverage is not established for this connector. | None verified. |
| 4 | Jupyter / JupyterLab | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/jupyter.md) | Interactive notebooks | Python and R notebook kernels can load their respective ADBC client libraries. [ADBC client libraries](https://arrow.apache.org/adbc/main/client_libraries.html). | Tested notebook execution in a separate Python kernel: stored rows, Arrow types, NULLs/Unicode, and saved notebook outputs. [Evidence](sdlc/changes/candidate-test-runner/validation.md). JupyterLab browser UI remains untested. | Tested 2026-11-03: Tested notebook execution in a separate Python kernel: Flight SQL query, Arrow schema/rows, NULLs/Unicode and saved notebook output. JupyterLab browser UI untested. [Flight evidence][flight-workflows]. | None verified. | No dedicated Redis notebook connector verified. |
| 5 | scikit-learn | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/scikit-learn.md) | Machine learning | Can consume data prepared by pandas, Polars, or NumPy; no native database connector. [Python Developers Survey](https://lp.jetbrains.com/python-developers-survey-2024/) tracks it among widely used ML libraries. | Tested a Redis ADBC Arrow result converted to NumPy arrays, then used to fit and predict with a small decision tree; custom pipeline only. | Tested 2026-11-03: Tested a small decision-tree fit/predict on stored rows fetched through Flight SQL and converted to NumPy; custom local pipeline, no packaged connector. [Flight evidence][flight-core]. | None verified. | No dedicated Redis connector verified. |
| 6 | PyTorch | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/pytorch.md) | Deep learning | Database access is typically implemented in application data pipelines; ADBC Python clients can supply Arrow batches for conversion. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested a custom Dataset/DataLoader and CPU model forward pass from fetched ADBC rows, with an explicit NULL mask. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested Flight SQL rows → custom Dataset/DataLoader and CPU model forward pass, with checked values and explicit NULL mask. No distributed/training-scale claim. [Flight evidence][flight-models]. | None verified. | No dedicated Redis connector verified. |
| 7 | TensorFlow | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/tensorflow.md) | Machine learning framework | Database access is typically implemented in input pipelines; ADBC Python clients can supply Arrow data for conversion. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested a custom `tf.data.Dataset` input pipeline and batched values/NULL mask from fetched ADBC rows. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested Flight SQL rows → custom `tf.data.Dataset`, checked batches, numeric values and NULL mask. Local CPU input pipeline only. [Flight evidence][flight-models]. | None verified. | No dedicated Redis connector verified. |
| 8 | Apache Airflow | ✅ 🔌 ✈️ 🤝 | 📖 ✍️ hook / tasks | Workflow orchestration | **Yes, through the Apache Arrow provider's `AdbcHook`.** Install the provider and database driver. [ADBC connection](https://airflow.apache.org/docs/apache-airflow-providers-apache-arrow/stable/connections/adbc.html). | Locally tested with Airflow 3.3.2 TaskFlow and Arrow provider 0.1.1: ingest, SQL aggregation and cleanup. The packaged `RedisAdbcHook` validates `get_records()` / `get_first()`, bound queries, and writes using autocommit and bundled-driver discovery. Import it explicitly inside a Python task; the generic Arrow provider 0.1.1 hook still rejects `get_records()` transaction mode. [Hook guide](python-integrations.md#redis-hook-for-get_records). [Setup and scope](python-integrations.md#apache-airflow). | Tested 2026-11-03: Tested a real local TaskFlow DAG with a task-local bound Flight SQL query, XCom handoff, downstream checks and successful task states. Generic AdbcHook and deployed scheduler/executor untested. [Flight evidence][flight-airflow]. | None verified. | [Apache Airflow Redis provider / RedisHook](https://airflow.apache.org/docs/apache-airflow-providers-redis/stable/_api/airflow/providers/redis/hooks/redis/index.html), Apache Airflow: get_conn() exposes a native Redis client for command calls. |
| 9 | Polars | ✅ 🔌 ✈️ | 📖 ✍️ ADBC | Fast DataFrame processing | **Yes, directly.** Database read/write APIs support ADBC connections. [Integration guide](https://arrow.apache.org/adbc/main/integrations.html). | **Locally validated, 2026-10-29:** connection/URI reads, typed write/append round trips, filtering and aggregation, NULLs, and empty-result schemas. The unchanged [Polars regression suite](../python/tests/test_polars.py) plus driver tests passed all 8 cases with warnings treated as errors on Redis Software 8.6.2 with verified 4x QPF. [Evidence and reproduction](sdlc/changes/polars-regression-rerun/validation.md). | Tested 2026-11-03: Tested `pl.read_database` with an autocommit ADBC Flight SQL connection and exact typed/NULL/Unicode rows. This sweep does not establish a Flight SQL destination. [Flight evidence][flight-core]. | None verified. | No dedicated Redis connector verified. Custom extraction can feed [polars.from_dicts](https://docs.pola.rs/api/python/stable/reference/api/polars.from_dicts.html). |
| 10 | dbt | ✅ 🔌 | 📖 ✍️ adapter | SQL transformations | **Yes, through adapters.** The Rust-based dbt v2 engine uses ADBC; dbt Core v1 can use it through a database-specific Python adapter. An ADBC driver alone does not register a database in either engine. [ADBC requirements](https://docs.getdbt.com/docs/dbt/adbc), [Adapter development](https://docs.getdbt.com/docs/contribute-dbt-adapters-v2?version=2). | Available: [dbt-redis-adbc for Core 1.10–1.x](https://github.com/redis/redis-dbt-adapter/tree/main/v1/adapter) and an [experimental native v2 adapter](https://github.com/redis/redis-dbt-adapter/tree/main/v2) compiled into the [custom engine fork](https://github.com/alberttwong/dbt). The standard v2 binary does not include Redis; install the native Redis driver separately. [Validation scope](https://github.com/redis/redis-dbt-adapter/blob/main/v2/VALIDATION.md). The current native v1 adapter also passed a local build, model-test, and catalog smoke against this checkout; [native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Unverified. The existing adapters load the Redis driver directly; they do not establish Flight SQL compatibility. Adapter connection/configuration work and end-to-end validation remain necessary. | [Redis-hosted v1/v2 adapter project](https://github.com/redis/redis-dbt-adapter), moved to the redis organization; see the project for support scope. | The experimental v2 Rust implementation remains in the [custom engine fork](https://github.com/alberttwong/dbt); the standard dbt v2 binary excludes Redis. |
| 11 | Apache Kafka / Kafka Connect | ✅ 🧩 | 📖 ✍️ sources / sinks | Event streaming and integration | **No standard ADBC connector verified.** The common Confluent database connector uses JDBC. [JDBC connector](https://docs.confluent.io/kafka-connectors/jdbc/10.8/overview.html). | Custom Kafka Connect connector required; the JDBC connector cannot directly load an ADBC library. | Candidate through a JDBC source connector plus Flight SQL JDBC; verify polling/incremental SQL and type mappings. Not CDC. | [Redis Kafka Connector](https://redis-field-engineering.github.io/redis-kafka-connect/), Redis Field Engineering: sinks and keys/Streams sources; full command API / Query Engine access not established. | Confluent JDBC connector is a Flight SQL candidate, not a verified unrestricted Redis command connector. |
| 12 | Apache Flink | ✅ 🧩 | 📖 ✍️ Streams source; Streams/JSON/Hash sinks | Stream processing | **No built-in generic ADBC connector verified.** Its documented relational database connector uses JDBC. [JDBC connector](https://nightlies.apache.org/flink/flink-docs-stable/docs/connectors/table/jdbc/). | Custom source/sink integration required; untested. | Candidate through the JDBC connector plus Flight SQL JDBC; validate dialect, types, batching, and streaming semantics. | [Redis Flink connector](https://github.com/redis-field-engineering/redis-flink-connector-dist), Redis Field Engineering / Redis, Inc.: Streams sources and Streams/JSON/Hash sinks. Full command API / Query Engine access not established. | Apache Flink JDBC connector is a Flight SQL candidate; check dialect and runtime compatibility. |
| 13 | Apache Beam | ✅ 🔌 | 📖 ✍️ Java RedisIO; [custom Python ADBC output](integrations/beam.md) | Batch and streaming pipelines | **Custom Python DoFn integration tested.** No built-in generic ADBC I/O verified; documented database I/O also includes JDBC. [JDBC I/O](https://beam.apache.org/releases/pydoc/current/apache_beam.io.jdbc.html). | Tested direct ADBC reads in local FnApi worker subprocesses: bound queries, populated/empty results, rows, Arrow schemas, NULLs, and Unicode. Two cases passed; no packaged I/O connector or remote-runner support implied. [Evidence](sdlc/changes/native-candidate-followup/validation.md). | Candidate through Java JdbcIO / Python JDBC I/O with Flight SQL JDBC; runner packaging and query/write behavior need validation. | None verified. | [Java RedisIO](https://beam.apache.org/releases/javadoc/current/org/apache/beam/sdk/io/redis/RedisIO.html), Apache Beam: key/value reads/writes and Streams writes. Full command API / Query Engine access not established. |
| 14 | Trino | ✅ 🧩 | 📖 Redis SQL Trino | Distributed SQL queries | **Driver available** for querying Trino; this is distinct from a Trino connector to an arbitrary ADBC source. [Driver list](https://arrow.apache.org/adbc/main/driver/index.html). | No generic Redis ADBC source connector verified; Trino would need connector work. | No built-in Redis Flight SQL connector verified; a custom Trino connector would be needed. | [Redis SQL Trino](https://github.com/redis-field-engineering/redis-sql-trino), built by Redis Field Engineering / Redis, Inc.: SQL over indexed HASH/JSON data with filters and aggregations pushed to the Query Engine. [Documentation](https://redis-field-engineering.github.io/redis-sql-trino/) explicitly describes FT.SEARCH / FT.AGGREGATE use and Redis Cluster configuration. This is separate from the Trino project's built-in Redis connector; unrestricted native command passthrough is not established. | [Built-in Trino Redis connector](https://trino.io/docs/current/connector/redis.html), Trino project: selected key/value reads, no Redis Cluster. This is a different connector from Redis SQL Trino. |
| 15 | Dask | ✅ 🔌 | 📖 ✍️ [custom Arrow output](integrations/dask.md) | Parallel DataFrames and Python computation | **Via this repository's Redis adapter; no built-in generic ADBC reader verified.** Dask SQL readers use SQLAlchemy URIs; custom readers can use from_map. [SQL integration](https://docs.dask.org/en/stable/dataframe-sql.html), [Custom loading](https://docs.dask.org/en/latest/generated/dask.dataframe.from_map.html). | [adbc_driver_redis.dask.read_query](python-integrations.md#dask) provides lazy partitioned reads with worker-local ADBC connections and pandas Arrow dtypes. [Recorded local validation](sdlc/changes/dask-353/validation.md) includes two worker processes; caller supplies partition parameters and exact schema. | Custom from_map loading with adbc_driver_flightsql and pandas partitions; untested. The bundled Redis helper does not establish Flight SQL compatibility. | None verified. | This repository's [Dask adapter](../python/adbc_driver_redis/dask.py) supplies the ADBC path; Redis, Inc. authorship or support is not established. Native Redis command access is not claimed. |
| 16 | Ray / Ray Data | ✅ 🔌 | 📖 ✍️ SQL API; worker reader + materialized output | Distributed Python and ML data processing | **Via this repository’s Redis adapter; no built-in generic ADBC reader claimed.** Ray’s documented Datasource/ReadTask interface supports worker-side Arrow blocks. [Extension API](https://docs.ray.io/en/latest/data/api/doc/ray.data.Datasource.html). | [adbc_driver_redis.ray.read_query](python-integrations.md#worker-side-arrow-reader) plans live ADBC partitions and streams Arrow batches from worker-local connections. Source build with the ray extra, pinned to Ray 2.59.0; matching native library required on every worker. A dedicated private-CA Software profile passes 17 cases, including complete worker reads and certificate rejection without trust. [TLS evidence](sdlc/changes/integration-252-254/validation.md). [Local validation](sdlc/changes/ray-worker-164/validation.md). | Unverified. This adapter loads the Redis driver directly; it does not establish Flight SQL compatibility. | None verified. | This repository’s [Ray adapter](../python/adbc_driver_redis/ray.py) supplies the ADBC path; Redis, Inc. authorship/support and upstream Ray endorsement are not established. Live unordered reads; no shared snapshot or stable retries. |
| 17 | Apache NiFi | ❌ | 📖 ✍️ cache reads/writes only; excluded from ✅ criteria | Visual data flows | **No standard ADBC processor verified.** Its standard SQL processor uses JDBC. [ExecuteSQL](https://nifi.apache.org/components/org.apache.nifi.processors.standard.ExecuteSQL/). | Custom processor or script integration required; untested. | Candidate through JDBC processors and Flight SQL JDBC; validate JVM options, metadata, and result conversion. | None verified. | [Redis connection/cache services](https://nifi.apache.org/components/org.apache.nifi.redis.service.RedisDistributedMapCacheClientService/), Apache NiFi: Redis-backed caching. Full command API / Query Engine access not established. |
| 18 | Airbyte | ✅ 🔌 | 📖 Redis ADBC source; separate ✍️ marketplace destination | Data replication | **Implemented community source proposal.** The Redis (ADBC) connector consumes Arrow batches and emits Airbyte JSON/stdio; this is not native Arrow transport or a generic built-in ADBC connector. [Upstream draft PR #90323](https://github.com/airbytehq/airbyte/pull/90323). | Full-refresh reads and metadata discovery over driver-exposed SQL tables/views; Search and autocommit required. Local Linux ARM64 connector and PostgreSQL destination validation recorded; not yet enabled in Airbyte Cloud/OSS registries. [Scope and evidence](#airbyte-redis-adbc-source). | Optional `flight_sql` backend tested against the SQLite reference server. A Redis-backed Flight SQL gateway and production authentication remain unvalidated; no ✈️ badge is inferred. | None verified. | Community [Redis (ADBC) source proposal](https://github.com/airbytehq/airbyte/pull/90323), contributed by alberttwong; Redis, Inc. maintenance/support not established. Separate [marketplace Redis destination](https://docs.airbyte.com/integrations/destinations/redis) writes data structures; it does not establish Query Engine access. |
| 19 | Fivetran | ✅ 🔌 | 📖 replication source | Managed data replication | **Custom SDK source, locally tested.** Its Python Connector SDK can consume Redis ADBC; hosted packaging/runtime compatibility remains unverified. [SDK guide](https://fivetran.com/docs/connector-sdk/getting-started). | Tested a custom native-ADBC source with the real local Connector SDK tester and verified DuckDB warehouse output. The acceptance harness records **1 passed** with SDK 2.13.1; hosted deployment and binary packaging remain unverified. [Acceptance evidence](sdlc/changes/integration-acceptance/validation.md). | Custom Connector SDK source using the Python Flight SQL driver; untested. | None verified. | [Community Redis Connector SDK example](https://github.com/fivetran/community_connectors/tree/main/redis), Fivetran community: reads keyspace/TimeSeries data for replication. Does not expose unrestricted Redis API or Query Engine commands as a tool connector; custom SDK code could be extended. |
| 20 | dlt | ✅ 🔌 | 📖 custom finite Arrow resource; ✍️ append destination | Python data loading | **Yes, for selected destinations and custom integrations.** This repository supplies an ADBC ingestion destination. [Guide](python-integrations.md#dlt). | Tested the repository append-only `adbc_driver_redis.dlt.redis_adbc` destination: all 12 cases passed in the current native sweep on Redis Software with verified 4x QPF. Install the `dlt` extra; replace/merge remain unsupported and retries can duplicate rows. [Guide](python-integrations.md#dlt), [current evidence](sdlc/changes/candidate-test-runner/native-sweep.md). | No Flight SQL destination validated; custom Python resource loading remains untested. | None verified. | This repository's [dlt destination](../python/adbc_driver_redis/dlt.py) supplies the ADBC path; no Redis, Inc. maintenance or support claim. Append only; replace/merge are rejected, and retries can duplicate partially written rows. |
| 21 | SQLAlchemy | ✅ 🔌 ✈️ | 📖 ✍️ autocommit SQL | Python SQL toolkit and ORM | External dialect projects include Redis via ADBC, but SQLAlchemy requires dialect support and the dialect's capabilities still need validation. [External dialect list](https://docs.sqlalchemy.org/en/20/dialects/). | The supported analytics dialect passed SQLAlchemy 2.1.6’s external-dialect suite: **677 passed, 683 skipped**, zero failures/errors. Reads, writes, typed parameters, DDL and reflection are covered within declared capabilities; transactions, autoincrement, SQL indexes and relational constraints remain unsupported. [Acceptance evidence](sdlc/changes/integration-acceptance/validation.md). | Tested 2026-11-03: Tested sqlalchemy-adbc 0.3.0 Flight SQL dialect reads and empty results with `connect_args={"autocommit": True}`. The dialect's `isolation_level="AUTOCOMMIT"` setter raises `NotImplementedError`; writes/reflection/ORM acceptance not covered by this Flight test. [Flight evidence][flight-core]. | None verified. | [adbc-driver-redis](https://docs.sqlalchemy.org/en/20/dialects/) appears in SQLAlchemy's external dialect list; third-party status and maintenance should be checked. |
| 22 | Apache Arrow PyArrow | ✅ 🔌 ✈️ | 📖 ✍️ Arrow fetch / ingest | Arrow data processing | PyArrow is the Arrow interchange layer used by Python ADBC clients; it provides Arrow arrays/tables and stream interfaces. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | 12 array/IPC/union-stream interface cases passed against the native driver, in addition to Arrow fetch/ingest across the application suites. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested Flight SQL Arrow batch fetch, exact typed/NULL/Unicode rows and empty-result schema. Fixture ingestion is setup evidence, not a packaged application connector. [Flight evidence][flight-core]. | None verified. | No dedicated Redis connector verified. |
| 23 | R / DBI | ✅ 🔌 ✈️ | 📖 ✍️ DBI helper; binding fixes require patched dependencies | Statistical computing | R's `adbi` package provides a DBI-compliant interface backed by ADBC. [R DBI backends](https://r-dbi.org/backends/), [ADBC R docs](https://arrow.apache.org/adbc/main/r/). | The sourced Redis DBI helper passed DBItest 1.8.3 with **9,385 passed, 14 skipped**, zero failures/errors, using pinned and patched adbi 0.1.4 and nanoarrow 0.9.0. Counts include individual expectations. Typed writes/binding and metadata are covered; transactions remain unsupported. Unmodified CRAN packages are not certified. [Acceptance evidence](sdlc/changes/integration-acceptance/validation.md), [dependency recipe](../r/install_acceptance.R). | Tested 2026-11-03: Tested R 4.6.2 driver-manager path with Flight SQL shared driver 1.13.0: DDL, INSERT, typed reads, NULLs, empty results, SUM and cleanup. DBI/adbi through Flight SQL remains untested. [Flight evidence][flight-documents]. | None verified. | No dedicated Redis DBI backend verified. |
| 24 | SciPy | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/scipy.md) | Scientific computing | ADBC can supply Arrow data that Python code converts to NumPy/SciPy structures; SciPy is not a database connector. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested fetched sparse-matrix coefficients, CSR construction, and a linear solve with checked solution/residual; custom local pipeline. [Evidence](sdlc/changes/candidate-test-runner/validation.md). | Tested 2026-11-03: Tested stored sparse coefficients fetched through Flight SQL → CSR matrix and checked linear solution/residual; custom local application pipeline. [Flight evidence][flight-workflows]. | None verified. | No dedicated Redis connector verified. |
| 25 | XGBoost | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/xgboost.md) | Gradient-boosted trees | Training data is normally provided as arrays, dataframes, or matrices; ADBC can supply data through Python first. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested native ADBC rows → NumPy/DMatrix, tiny CPU training with a missing value, prediction checks, and model save/reload. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested Flight SQL rows → NumPy/DMatrix, tiny CPU training with a missing value, checked predictions and model save/reload. No training-scale claim. [Flight evidence][flight-models]. | None verified. | No dedicated Redis connector verified. |
| 26 | Hugging Face Transformers | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/transformers.md) | Machine learning and language models | Python applications can fetch data through ADBC and convert it for model pipelines; Transformers itself is not a database connector. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested a local tokenizer and tiny randomly initialized BERT model from fetched labels, then offline model save/reload. No pretrained model/Hub download or training-scale validation. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested Flight SQL labels → local tokenizer and tiny randomly initialized BERT model, finite outputs and offline model save/reload. No pretrained model/Hub download or distributed training. [Flight evidence][flight-models]. | None verified. | No dedicated Redis Transformers connector verified. |
| 27 | MLflow | ✅ 🔌 | 📖 ✍️ [custom Arrow output](integrations/mlflow.md) | ML lifecycle management | Dataset tracking accepts Python dataframes. Tracking/artifact storage uses MLflow-specific backends; no generic ADBC tracking backend verified. [Dataset API](https://mlflow.org/docs/latest/api_reference/python_api/mlflow.data.html). | Tested custom native ADBC → Arrow/pandas → dataset tracking: populated/empty schema/profile, digest, recorded run inputs, and artifacts. Two workflow cases passed; empty data uses an explicit digest and the exact default-digest failure remains a strict expected failure. This does not provide a Redis MLflow tracking store. [Evidence](sdlc/changes/native-candidate-followup/validation.md). | No Flight SQL integration verified. | None verified. | No qualifying Redis connector verified. |
| 28 | Streamlit | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/streamlit.md) | Data apps and dashboards | Python app code can use ADBC directly; Streamlit does not provide a database-specific ADBC adapter. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested an AppTest app displaying stored Arrow rows/metrics and a bound-filter input rerun with NULLs/Unicode. [Evidence](sdlc/changes/candidate-test-runner/validation.md). Browser/hosting remains untested. | Tested 2026-11-03: Tested a Flight SQL-backed AppTest app: stored typed rows/NULLs/Unicode, dataframe/metric display and bound-filter input rerun. Browser rendering and hosting untested. [Flight evidence][flight-workflows]. | None verified. | Custom Redis SDK code is possible but is not a packaged Streamlit connector. |
| 29 | Plotly | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/plotly.md) | Interactive visualization | Python application code can use ADBC to fetch data before plotting; Plotly is not a database connector. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested fetched rows with NULLs/Unicode → figure JSON save/reload and self-contained interactive HTML serialization; browser interaction untested. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested Flight SQL rows → figure JSON save/reload and self-contained HTML with checked numeric, NULL and Unicode values. Browser interaction untested. [Flight evidence][flight-models]. | None verified. | No dedicated Redis connector verified. |
| 30 | Matplotlib | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/matplotlib.md) | Plotting and visualization | Can plot data fetched by Python ADBC clients; no database connection layer of its own. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested fetched rows → Agg rendering, checked plotted numeric values/NULL gap, and verified a nonblank PNG. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested Flight SQL rows → Agg rendering, checked numeric values/NULL gap and nonblank PNG. Custom plotting pipeline only. [Flight evidence][flight-models]. | None verified. | No dedicated Redis connector verified. |
| 31 | Tableau | ✅ ✈️ | 📖 Flight SQL JDBC preview | BI and visualization | No generic ADBC connector verified; database connectivity relies on supported connectors and its extension APIs. [Tableau connectors](https://www.tableau.com/products/connectors). | No Redis ADBC loading path verified in Tableau. | Tested 2026-11-03: Tested Desktop Free Edition 2026.3 / Flight SQL JDBC 19.0.0: new connection, catalog/schema/table/column discovery and generated three-row preview including NULL. Worksheets, filters/aggregates, extracts, dashboards, joins, writes and TLS/authentication untested. [Flight evidence][flight-tableau]. | None verified. | Generic Tableau JDBC connector with Apache Arrow Flight SQL JDBC; no native Redis Tableau connector claimed. |
| 32 | Apache Superset | ✅ 🔌 | 📖 analytics / SQL Lab query path | BI and data exploration | Database connections use SQLAlchemy engines/dialects; not a generic ADBC consumer. [Superset database connections](https://superset.apache.org/docs/configuration/databases/). | Tested the repository SQLAlchemy dialect/engine spec with real dataset metadata, chart aggregation/time-series SQL, and SQL Lab execution in a temporary metadata database; 1 application test passed. Browser dashboards and hosting remain untested. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Flight SQL JDBC/ADBC is not a direct Superset SQLAlchemy backend. | None verified. | This repository’s SQLAlchemy dialect/engine spec supplies the locally tested path; no separate native Redis command connector is claimed. |
| 33 | Metabase | ✅ ✈️ | 📖 ✍️ Flight SQL API / SQL Actions | BI and analytics | Database support is provided through built-in or community database drivers, not generic ADBC loading. [Metabase database drivers](https://www.metabase.com/docs/latest/databases/connections/). | A Metabase database driver is required; no Redis SQL driver verified. | Tested 2026-11-03: Tested 0.64.2.9 / community Flight SQL plugin 0.8.0 / JDBC 19.0.0: API reads/NULL/empty/aggregates, metadata sync, generated filters/grouping and INSERT/UPDATE/DELETE Actions with read-back. Browser dashboards untested; native DDL via the read endpoint is not a supported write path. [Flight evidence][flight-metabase]. | None verified. | Community Flight SQL plugin 0.8.0 with bundled JDBC 19.0.0; not a Redis-built connector. |
| 34 | Dagster | ✅ 🔌 ✈️ | 📖 ✍️ IO manager / resource | Data orchestration | **Yes, through a community integration.** `dagster-adbc` provides `ADBCResource`. [Integration guide](https://docs.dagster.io/integrations/libraries/adbc). | **Tested repository integration:** `adbc_driver_redis.dagster.RedisADBCIOManager` supports Arrow, pandas, Polars, and reader inputs/outputs, including partition tests. Install the `dagster` extra; [guide](python-integrations.md#dagster). The community `dagster-adbc` 0.0.2 `ADBCResource` is also locally validated with manifest or library-path discovery and explicit `autocommit=True`; [evidence](sdlc/changes/dagster-adbc-resource/validation.md). | Tested 2026-11-03: Tested community dagster-adbc ADBCResource with the Flight SQL shared driver, autocommit, materialized asset, exact rows and a bound query. Redis-specific IO manager and distributed execution not validated through Flight SQL. [Flight evidence][flight-core]. | None verified. | Community dagster-adbc supplies generic ADBC integration. This repository's Redis-specific IO manager is listed in the Redis ADBC column; its presence does not establish Redis, Inc. maintenance. No separate native-command connector verified. |
| 35 | Prefect | ✅ 🔌 🤝 | 📖 ✍️ Python tasks | Python workflow orchestration | **Custom Python integration.** Tasks can call ADBC libraries; a dedicated ADBC connector was not verified. [Python task API](https://reference.prefect.io/prefect/tasks/). | **Tested with Prefect 3.9.0 and Redis Software 8.6.2:** local flows, Arrow ingestion/fetch, dependencies, concurrent queries, retries, and SQL errors. [Usage](python-integrations.md#prefect-custom-python-tasks), [test evidence](sdlc/changes/prefect-integration/plan.md). | Custom Python tasks using adbc_driver_flightsql; untested. | None verified. | [prefect-redis](https://docs.prefect.io/integrations/prefect-redis), Prefect: RedisDatabase, locks, and messaging. Its [RedisDatabase.get_client()](https://reference.prefect.io/prefect_redis/blocks/#prefect_redis.blocks.RedisDatabase.get_client) exposes a native Redis client for direct command calls; this tool-specific integration is distinct from installing redis-py alone. |
| 36 | Mage | ✅ 🔌 | 📖 ✍️ [custom Arrow output](integrations/mage.md) | Data pipeline authoring | Custom Python loader blocks can invoke ADBC. [Mage integrations](https://docs.mage.ai/integrations). | Tested a real local loader/transformer pipeline: native bound queries, populated/empty dataframe handoff, values, dtypes, NULLs, and Unicode. Two cases passed; no packaged connector or hosted deployment implied. [Evidence](sdlc/changes/native-candidate-followup/validation.md). | Custom block/resource required; untested. | None verified. | No qualifying Redis connector verified. |
| 37 | Kestra | ✅ 🔌 | 📖 ✍️ [custom Arrow output](integrations/kestra.md) | Workflow orchestration | ADBC driver loading works in a custom Python Script task; it is not a generic database plugin path. [Kestra plugins](https://kestra.io/plugins). | Tested Python Script tasks on local Kestra 2.0.7: native bound queries, populated/empty Arrow schema/rows, managed file handoff to a downstream checker, and retrieved task outputs. Two cases passed. [Evidence](sdlc/changes/native-candidate-followup/validation.md). | Custom Flight SQL task/plugin required; untested. | None verified. | [Kestra Redis plugin](https://kestra.io/plugins/plugin-redis) supports Redis data structures and pub/sub; it does not establish general SQL/Query Engine access. |
| 38 | Ibis | ✅ 🔌 | 📖 ✍️ [custom Arrow output](integrations/ibis.md) | Python analytics API | `ibis.memtable()` accepts fetched Arrow tables. [API](https://ibis-project.org/reference/expression-tables#memtable). | Tested custom native ADBC → Arrow → Ibis expressions on a local DuckDB backend: populated/empty schema/rows, NULLs/Unicode, filters, counts, and sums. Two cases passed; this does not supply a Redis Ibis backend or pushdown into Redis. [Evidence](sdlc/changes/native-candidate-followup/validation.md). | No Redis Flight SQL backend verified. | None verified. | No qualifying Redis connector verified. |
| 39 | Great Expectations | ❌ | ⚠️ **Unverified** — no Redis path verified | Data quality | Database validation generally depends on supported SQLAlchemy connections and execution engines, not generic ADBC. [GX data sources](https://docs.greatexpectations.io/docs/core/connect_to_data/sql_data/). | Requires supported SQL backend/dialect integration; untested. | No native Flight SQL path verified. | None verified. | No qualifying Redis connector verified. |
| 40 | Soda | ❌ | ⚠️ **Unverified** — no Redis path verified | Data quality | SQL data sources use Soda's supported data source integrations; generic ADBC consumption not verified. [Soda data sources](https://docs.soda.io/soda-cl/data-sources.html). | Requires a Redis SQL data source implementation; untested. | No direct Flight SQL path verified. | None verified. | No qualifying Redis connector verified. |
| 41 | Microsoft Power BI | ❌ | 📖 CData / Power BI; excluded from ✅ criteria | BI and visualization | Power Query documents custom ADBC connectors; a Redis connector still needs to be developed and validated. [ADBC connector development](https://learn.microsoft.com/en-us/power-query/adbc). | No packaged Redis ADBC connector verified. | Custom Flight SQL connector candidate; untested. | None verified. | [CData Redis Power BI connector](https://www.cdata.com/kb/tech/redis-powerbi-setup.rst) is commercial and ODBC-based; full command / Query Engine access not established. |
| 42 | Keras | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/keras.md) | Deep learning API | Training inputs can be supplied by application code using Python ADBC and Arrow; Keras is not a database connector. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested fetched native ADBC values as CPU model inputs, explicit missing-value handling, checked predictions, and `.keras` save/reload using the TensorFlow backend. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested Flight SQL rows → CPU model inputs, explicit missing-value handling, checked predictions and `.keras` save/reload with TensorFlow backend. No training-scale claim. [Flight evidence][flight-models]. | None verified. | No dedicated Redis connector verified. |
| 43 | Kubeflow Pipelines | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/kubeflow.md) | Machine learning workflow orchestration | Python pipeline components can load ADBC clients, but Kubeflow does not supply a generic ADBC connector. [Kubeflow Pipelines overview](https://www.kubeflow.org/docs/components/pipelines/overview/). | Tested a custom component with KFP 2.18.0’s local SubprocessRunner: task-local native ADBC query, typed/NULL/Unicode assertions, and checked materialized output. Kubernetes/remote execution untested. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested custom Flight SQL component with local SubprocessRunner: task-local query, NULLs/Unicode/values and checked materialized output. Kubernetes/remote execution untested. [Flight evidence][flight-orchestrators]. | None verified. | No dedicated Redis Kubeflow connector verified. |
| 44 | Metaflow | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/metaflow.md) | Data science and ML workflow framework | Python tasks can use ADBC libraries; Metaflow itself does not provide a generic ADBC database connector. [Metaflow documentation](https://docs.metaflow.org/). | Tested a custom local flow with native ADBC reads, persisted rows between steps, and checked NULL/Unicode values. Remote backends untested. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested a custom local flow with task-local Flight SQL reads, persisted rows between steps and checked values/NULLs/Unicode. Remote backends untested. [Flight evidence][flight-orchestrators]. | None verified. | No dedicated Redis Metaflow connector verified. |
| 45 | Hugging Face Datasets | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/huggingface-datasets.md) | Dataset loading and preparation | The library uses Arrow tables and can bridge ADBC query results into model datasets through custom Python code. [Arrow integration](https://huggingface.co/docs/datasets/use_with_pyarrow). | Tested Arrow conversion, local save/reload/filter, types/NULLs/Unicode, and empty results. Datasets 5.1.0 needs `num_shards=1` for empty persistence; default empty saves have independent and ADBC regression coverage. A proposed upstream fix passes locally. [Follow-up](sdlc/changes/integration-252-254/validation.md). [Evidence](sdlc/changes/candidate-test-runner/validation.md). | Tested 2026-11-03: Tested Flight SQL Arrow conversion, populated save/reload/filter and empty typed persistence with `num_shards=1`. Default empty saves reproduce the exact Datasets 5.1.0 zero-shard defect as a strict expected failure; linked [Flight tracker](https://github.com/redis/redis-apache-arrow-flight/issues/57) and [upstream defect](https://github.com/huggingface/datasets/issues/8912). [Flight evidence][flight-workflows]. | None verified. | No dedicated Redis Datasets connector verified. |
| 46 | Quarto | ✅ 🔌 ✈️ | 📖 ✍️ [custom Arrow output](integrations/quarto.md) | Scientific and technical publishing | Quarto runs code in Python and R documents; those code cells can use ADBC clients, but Quarto is not a database connector. [Quarto FAQ](https://quarto.org/docs/faq/). | Tested a Python document rendered to HTML by Quarto 1.10.20 with native ADBC queries and checked stored rows/NULLs/Unicode. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Tested a Python document rendered to HTML by Quarto 1.10.20 with executed Flight SQL queries and checked stored rows/NULLs/Unicode. R document and hosted execution untested. [Flight evidence][flight-documents]. | None verified. | No dedicated Redis Quarto connector verified. |
| 47 | Meltano | ❌ | ⚠️ **Unverified** — no Redis path verified | ELT and data integration | Meltano orchestrates extractor and loader plugins; no generic ADBC connector verified. [Meltano integration guide](https://docs.meltano.com/guide/integration). | Custom tap/target or plugin work required; no packaged Redis ADBC connector verified. | Custom connector required; no generic Flight SQL plugin verified. | None verified. | No qualifying Redis ADBC or full-command connector verified. |
| 48 | Apache Hop | ❌ | ⚠️ **Unverified** — no Redis path verified | Visual data integration and orchestration | Hop builds and runs pipelines through its plugin ecosystem; no generic ADBC connector verified. [Apache Hop overview](https://hop.apache.org/manual/latest/getting-started/hop-what-is-hop.html). | Custom transform/connection plugin required; untested. | Custom Flight SQL integration required; untested. | None verified. | No qualifying Redis connector verified. |
| 49 | RStudio | ✅ 🔌 ✈️ | 📖 ✍️ R/DBI literal SQL; binding limits | R and Python development environment | RStudio IDE supports R and Python code; ADBC clients can be used from the corresponding language environment. [RStudio IDE guide](https://docs.posit.co/ide/user/index.html), [ADBC client libraries](https://arrow.apache.org/adbc/main/client_libraries.html). | Manually executed the saved native R/DBI harness in RStudio 2026.10.1 with R 4.6.2, including fixture creation, typed reads, empty results, and cleanup. Console success was verified; wider IDE workflows and the R binding limitation remain. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Tested 2026-11-03: Executed the Flight SQL R driver-manager harness in RStudio 2026.10.1+239 / R 4.6.2: DDL, writes, typed reads, NULLs, empty result, SUM and cleanup; console success observed and transcript saved. DBI/adbi, Connections pane and Python IDE workflow untested. [Flight evidence][flight-rstudio]. | None verified. | No dedicated Redis RStudio connector verified. |
| 50 | Dataform | ❌ | ⚠️ **Unverified** — no Redis path verified | SQL transformation workflows | Dataform develops and schedules SQL workflows for BigQuery; no generic ADBC driver consumer verified. [Dataform overview](https://docs.cloud.google.com/dataform/docs/overview). | Redis adapter/target would be required; Dataform standard workflows execute in BigQuery. | No direct Flight SQL integration verified. | None verified. | No Redis Dataform adapter verified. |

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

Start with the implemented **Polars, Dagster, Airflow, Prefect, and Ray Data** paths in the table.
The [Python integration guide](python-integrations.md) documents the published
[`adbc-driver-redis`](https://pypi.org/project/adbc-driver-redis/) package,
DB-API connection setup, TLS alias, and native driver manifests. Wheels bundle
the driver for Python 3.12+ (3.14 recommended for current source) on Linux amd64/arm64 (glibc 2.28+) and macOS arm64
(macOS 15+). Non-Python driver consumers still need a manifest or library path.
Redis with Search must run separately; see [server requirements](driver-reference.md#server-requirements).
Writes autocommit, and the driver does not provide transaction rollback or isolation.

Direct pandas `read_sql` / `read_sql_query`, Polars reads/writes, and Dagster's
community resource now have dedicated validation records from October 29.
Prefect custom Python tasks also have an October 29 local validation record.
Those results supplement the earlier IO-manager and DuckDB evidence; they do
not establish Flight SQL compatibility or every package/platform combination.

Fabric / Power Query is another candidate with custom connector work. dbt has
community Redis adapters for Core 1.10–1.x and experimental v2 support in a custom
engine fork. The v1 Python package does not enable Redis in the standard v2
executable. Both documented paths load the native Redis ADBC driver directly;
Flight SQL remains unverified.
dlt now has this repository's append-only Redis ADBC destination. It does not
provide replace/merge, automatic schema migrations, rollback, or exactly-once
retries. Local pipeline state must be retained; see the [dlt guide](python-integrations.md#dlt).
Fivetran has a locally exercised custom Connector SDK source; hosted deployment
and binary packaging remain unverified.

Ray is ✅ 🔌 through this repository's worker-side Arrow reader. It uses
live ADBC partition descriptors, closes planning handles before worker reads,
and streams batches from independent worker connections. Install a build with
the `ray` extra and matching native library on every worker. SQL restrictions,
Search/ACL requirements, unordered live reads and retry limits remain explicit
in the [Ray guide](python-integrations.md#worker-side-arrow-reader).
Upstream acceptance and broader rollout remain tracked in
[#164](https://github.com/redis/redis-adbc-driver/issues/164).

Dask is ✅ 🔌 through
this repository's partitioned read adapter. Its caller must supply disjoint,
exhaustive partition predicates and an exact Arrow schema; each partition is
materialized in worker memory, and tasks do not share a snapshot. Install the
package and native driver on every worker. See the
[Dask partitioning contract](python-integrations.md#dask).

The package also ships [SQLAlchemy and Superset integrations](python-integrations.md#sqlalchemy-and-superset),
included in the current 50-tool selection. Their recorded checks cover reflection, bound SQL,
Superset discovery, dataset queries, and the SQL Lab cursor path; they do not
establish browser-rendered chart coverage or transactional ORM support.

## Airbyte Redis ADBC source

Status checked **2026-11-01**: [Airbyte PR #90323](https://github.com/airbytehq/airbyte/pull/90323)
is an open **draft community contribution**, with Cloud and OSS registry entries
disabled pending review. The [pinned connector guide](https://github.com/alberttwong/airbyte/blob/5c20a3f6e218d27c3e2f889e3cec53dbf8449453/docs/integrations/sources/redis-adbc.md)
describes building an image and registering it as a custom source in self-hosted
Airbyte. This implementation qualifies the Airbyte row through Redis ADBC; it is
not a released or officially supported Airbyte catalog connector.

The source discovers driver-exposed SQL tables/views, selects streams and
top-level fields, and performs **Full Refresh** reads for destination Overwrite
or Append. Redis Software/Cloud with Search and autocommit are required; arbitrary
application HASHes are not automatically adopted. The proposed image pins
`adbc-driver-redis==0.0.19`, whose wheel bundles the native library.
Arrow batches are converted to Airbyte JSON/stdio records, preserving NULLs,
64-bit integers and decimals (the latter two as exact strings), binary as Base64,
and timestamp precision. This does not add native Arrow transport or establish
a performance improvement.

The [upstream validation report](https://github.com/airbytehq/airbyte/pull/90323)
records **54 connector tests** (50 unit/CLI, three native Redis, one actual Flight
SQL reference-server test), lint/type checks and an image build on Linux ARM64.
Two source-CLI → `airbyte/destination-postgres:3.0.23` → PostgreSQL 16.10
full-refresh syncs passed against disposable Redis Software 8.6.2 with Search,
noeviction and verified 4x QPF. Checks cover final field values, an empty stream
and Overwrite replay without duplicates. The separate CDK standard suite reported
**3 passed, 19 skipped, 18 deselected**; skipped live acceptance cases are not passes.
These are externally recorded connector results, separate from this repository's
historical 37-entry native sweep below; they were not rerun for this update.

With this source and the exercised custom Fivetran and Superset paths included,
the 50-row table now has **43 qualifying tools**, including candidates:
**37 Redis ADBC paths** and **27 Redis Flight SQL paths** (24 overlap ADBC).
The November 3 Flight validation below adds Tableau and Metabase and records
scoped executions for all 27 Flight routes; it does not certify all applications.
Fivetran and Superset qualify through their implemented ADBC integrations;
this correction does not establish hosted Fivetran or browser-dashboard validation.
The historical 34-guide bidirectional check above predates this read-only source
addition; it does not establish an Airbyte Redis write path.

The optional Flight SQL backend was tested with Apache Arrow Go 18.8.0's SQLite
reference server. A production Redis-backed Flight SQL gateway, TLS/ACL and Flight
authentication combinations, AMD64 execution, and a deployed Airbyte platform
remain unvalidated. There is no incremental state, CDC, shared snapshot, arbitrary
key scan or Redis Streams consumer. Concurrent writes and TTL expiry can affect
reads; failed Append retries can duplicate records. The existing marketplace
Redis destination is a separate integration and does not make this source a
Redis write path.

## Testing status

### Flight SQL validation — November 3, 2026

All **27 Flight SQL routes** in the 50-tool inventory now have recorded scoped
executions: **21 formerly candidate routes** plus the six previously exercised
pandas, Polars, PyArrow, R driver-manager, Tableau and Metabase routes. Twenty-four
tool routes are automated; RStudio and Tableau were checked in their native UIs,
and Metabase through its application API. The final run recorded **28 automated
passes and two strict expected failures**, separately from the three application
checks. These are test-case counts, not additional tool entries.

The [saved Flight validation][flight-validation], [tool-level summary][flight-summary],
[repeatable runner and prerequisites][flight-runner], and
[Flight PR #58](https://github.com/redis/redis-apache-arrow-flight/pull/58)
record the exact scope and evidence. Links below pin the published evidence to
an immutable Flight commit, so they do not depend on that PR being merged.
The 50 rows now include **43 qualifying tools**, with **37 native ADBC paths**
and **27 Flight SQL paths** (24 overlap). DuckDB, DBeaver, Go and Java have
separate Flight evidence but are outside this 50-tool count. Airbyte's SQLite
reference-server test remains excluded from Redis-backed Flight validation.

This historical run used community Flight source `c019afc`, pinned server-side
Redis ADBC `2f053d3affad`, and standalone Redis Open Source 8.6.2 with Search.
It was not a Software + verified 4× QPF run and does not qualify the current
Enterprise TLS/authentication gate. Python clients used ADBC Flight SQL/manager
1.13.0 and PyArrow 26.0.0; resolved package and interpreter versions are retained
per profile. Spark and the BI applications used Flight SQL JDBC 19.0.0.
No driver implementation or current Software validation was rerun for this
inventory update.

The two strict expected failures reproduce the same Datasets 5.1.0 default
empty-save zero-shard defect: an independent Arrow-only case and a Flight SQL
empty-result case. The explicit `num_shards=1` workaround and nonempty controls
passed. Track removal of those expectations in [Flight #57](https://github.com/redis/redis-apache-arrow-flight/issues/57),
linked to [Datasets #8912](https://github.com/huggingface/datasets/issues/8912).
Expected failures are not passes. Separate model interpreters avoid a reproduced
native-runtime abort; Quarto requires the pinned PyYAML dependency.

Passes apply to the named versions and operations. Small custom data preparation
pipelines do not supply native connectors, distributed/cloud deployment,
production retry guarantees or full application certification. R is
driver-manager coverage, not DBI/adbi through Flight SQL. Browser dashboards,
Tableau worksheets/extracts, Flight TLS/authentication and large-scale workloads
remain outside the recorded scope. Direct-ADBC test records below retain their
original scope and are not replaced by this separate Flight run.

### Latest merged PR evidence — November 2, 2026

- [#282](https://github.com/redis/redis-adbc-driver/pull/282) adds a
  [50-tool upstream acceptance audit](integrations/acceptance.md), sequential
  `make -C go acceptance` runner, and SQLAlchemy/DBI compatibility fixes.
  The final [record](sdlc/changes/integration-acceptance/validation.md) and
  [machine-readable results](sdlc/changes/integration-acceptance/fixed-results.json)
  report the following; skips remain separate from passes.

| Contract | Passed | Skipped | Scope |
|---|---:|---:|---|
| SQLAlchemy 2.1.6 | 677 | 683 | Supported analytics dialect; unsupported identity/index/constraint/transaction features excluded. |
| DBItest 1.8.3 | 9,385 | 14 | Sourced Redis helper, pinned/patched adbi 0.1.4 and nanoarrow 0.9.0; JUnit counts include individual expectations. |
| ADBC validation plus Redis tests | 477 | 8 | Native driver contract and Redis extensions. |
| Fivetran SDK 2.13.1 local tester | 1 | 0 | Earlier harness run; stored warehouse values checked, not rerun for the final dialect/DBI fixes. |

All rows recorded zero failures/errors. These are separate suites and revisions;
counts are not a single combined compatibility score. The final three-suite
rerun used macOS arm64, a rebuilt native library and an owned Redis Software
8.6.2 fixture with Search, authenticated verified TLS, noeviction and active
verified 4x QPF. All five Go packages also passed the live regression suite.
Linux/hosted CI and released wheels are separate evidence. DBI’s patched
dependencies and the dialect’s declared exclusions prevent any full DBI or
PostgreSQL compliance claim. Superset retains its separate application smoke
evidence; SQLAlchemy acceptance does not certify dashboards or other SQL tools.

- [#281](https://github.com/redis/redis-adbc-driver/pull/281) adds
  [installation routes for all 50 entries](integration-installation.md),
  pandas/Polars extras, release-gated DuckDB fallback binaries and the checksummed
  managed installer, plus automatic PyPI publication after a qualifying successful
  binary release. [Validation](sdlc/changes/integration-distribution/validation.md)
  records **29 focused checks**, a macOS arm64 build/test wheel and publication
  gate checks. This is merged automation, not evidence that a subsequent complete
  release succeeded. dbt/Airbyte releases and dbc/DuckDB registry publication
  remain separate.
- [#280](https://github.com/redis/redis-adbc-driver/pull/280) ports authenticated
  TLS Enterprise integration fixtures onto current main. Its original-checkout
  test counts are not current-main results; #282 supplies the newer scoped local
  acceptance evidence above.
- [#275](https://github.com/redis/redis-adbc-driver/pull/275) records the
  bidirectional guide examples described above; [#279](https://github.com/redis/redis-adbc-driver/pull/279)
  adds the separate read-only Airbyte source proposal and current dbt links.

### Earlier candidate and application evidence

The earlier complete candidate sweep finished on **2026-10-30**: **152 passed,
one plaintext-group TLS skip, and seven strict expected-failure cases** (four
R/DBI bindings, two Datasets default empty saves, and one MLflow empty digest).
The dedicated private-CA Ray profile passed all 17 cases without skips. All fourteen profiles
exited 0 against Redis Software 8.6.2 with active verified 4x QPF. The
[five-tool follow-up and complete results](sdlc/changes/native-candidate-followup/validation.md)
add Ibis, MLflow, Beam, Mage, and Kestra to the
[earlier 32-entry accounting](sdlc/changes/candidate-test-runner/native-sweep.md):
**37 of 50 entries had exercised native paths in that sweep**, including the previously
recorded manual RStudio check. The remaining thirteen rows were outside that native sweep or needed
integration work. Flight SQL was excluded from that historical sweep at the
user's request; the separate November 3 Flight results are recorded above.

The earlier results below remain **historical evidence from their linked PRs**,
separate from the complete rerun above:

The separately recorded [R/Datasets/Ray TLS follow-up](sdlc/changes/integration-252-254/validation.md)
passes 17 Ray TLS cases without skips and validates proposed dependency fixes in
isolated copies. Released adbi/Datasets retain their exact expected failures.

- [#226](https://github.com/redis/redis-adbc-driver/pull/226): initial direct pandas
  query reads and SQLAlchemy checks, **4 passed**. [Record](sdlc/changes/pandas-read-sql/validation.md).
- [#227](https://github.com/redis/redis-adbc-driver/pull/227): community Dagster
  resource and existing IO manager, **14 passed**, zero skips. [Record](sdlc/changes/dagster-adbc-resource/validation.md).
- [#228](https://github.com/redis/redis-adbc-driver/pull/228): expanded direct pandas
  query reads, **9 passed**, zero skips. This includes the two pandas cases
  introduced in #226; counts are not additive. [Record](sdlc/changes/pandas-direct-reads/validation.md).
- [#229](https://github.com/redis/redis-adbc-driver/pull/229): unchanged Polars
  regression and driver tests, **8 passed**, zero skips, warnings treated as
  errors. [Record](sdlc/changes/polars-regression-rerun/validation.md).

- [#230](https://github.com/redis/redis-adbc-driver/pull/230): Prefect local flows
  and tasks, **4 passed**, zero skips after rebuilding on public main. Coverage
  includes Arrow ingestion/fetch, dependencies, concurrent queries, retry after
  a committed write, and driver-error propagation. [Record](sdlc/changes/prefect-integration/plan.md).

- [#231](https://github.com/redis/redis-adbc-driver/pull/231): Ray worker-side
  Arrow datasource, **17 passed**, zero skips in the final TLS suite, with three
  Ray DataContext deprecation warnings. [Record](sdlc/changes/ray-worker-164/validation.md).

- [#232](https://github.com/redis/redis-adbc-driver/pull/232): packaged
  `RedisAdbcHook` for autocommit query helpers and bundled-library discovery.
  **8 passed**, zero skips: five focused Airflow hook cases plus installation
  and autocommit regression checks. The earlier two TaskFlow smoke tests were
  preserved but were not rerun for that adaptation. [Record](sdlc/changes/airflow-get-records/validation.md).

- [#234](https://github.com/redis/redis-adbc-driver/pull/234): append-only dlt
  destination. Earlier source revisions recorded all **12 dlt tests** passing;
  the later pre-port rerun had **16 passed** including four driver tests. On
  public base `71625e1`, the library built and dlt tests reported **5 passed,
  7 skipped** without a live Redis fixture. That public-base check does not
  establish an end-to-end pipeline pass. [Record](sdlc/changes/dlt-integration/validation.md).
- [#235](https://github.com/redis/redis-adbc-driver/pull/235): pandas writes,
  nullable typed round trips, append, filtering/aggregation, and existing-table
  modes. Combined read/write validation: **12 passed**, zero skips on public
  base `71625e1`. This includes the nine earlier read cases, so totals are not
  additive. [Record](sdlc/changes/pandas-smoke/validation.md).

The #234 earlier live runs used macOS arm64, Python 3.12, dlt 1.31.0,
PyArrow 26.0.0 and ADBC manager 1.13.0. The later pre-port run verified Redis
Software 8.6.2 with active 4x QPF; the record distinguishes earlier sources
from the public base, where seven live cases skipped. A development wheel
packaging check is not evidence of a published native release wheel.
The #235 public-port run used macOS arm64, pandas 3.1.0, PyArrow 26.0.0,
ADBC manager 1.13.0, and Redis Software 8.6.2 with Search and verified 4x QPF.
Neither PR's record claims completed hosted CI or a full current-main suite.

The #232 current-main run used macOS arm64, Python 3.12.15, Airflow 3.3.2,
Arrow provider 0.1.1, common-sql 2.1.1, ADBC manager 1.12.0 and PyArrow 26.0.0.
Its Redis Software 8.6.2 fixture had verified active 4x QPF. TLS option
forwarding was checked; a live TLS handshake, Airflow 2.x and configured Linux
CI were not validated by that run.

The #231 run used macOS arm64, Python 3.12.14, Ray 2.59.0, PyArrow 26.0.0,
ADBC manager 1.13.0, and a disposable one-shard Redis Software 8.6.2 fixture
with Search, verified active 4x QPF, and verified TLS. Its two-CPU Ray runtime
used separate local worker processes, not local mode. This does not validate
multi-node deployments, Linux execution, release wheels, or performance.

The #230 run used macOS arm64, Python 3.15.0, Prefect 3.9.0, PyArrow 26.0.0,
ADBC manager 1.13.0, and a disposable one-shard Redis Software 8.6.2 RAM database
with Search and verified active 4x QPF. Remote workers, Prefect Cloud,
`prefect-redis`, Flight SQL, and Linux CI results were not validated by that run.

The final #227–#229 runs used macOS arm64, Python 3.12, ADBC manager 1.13.0,
PyArrow 26.0.0, and Redis Software 8.6.2 with verified active 4x QPF. Package
versions were Dagster 1.13.27 / dagster-adbc 0.0.2, pandas 3.1.0, and Polars 2.1.0,
respectively. Each record identifies its source revision and tested topology.
Older IO-manager, wheel, DuckDB, and Superset evidence remains linked to the
[2026-10-20 Python verification record](sdlc/changes/271-280/validation.md) and
[DuckDB record](sdlc/changes/duckdb-community/validation.md); those used different
versions and a disposable Redis 8.6.2 with Search, not the later Software/QPF setup.

| Path | Recorded evidence | Limits and remaining gaps |
|---|---|---|
| Direct pandas SQL reads/writes | [Read/write tests](../python/tests/test_pandas_writes.py) and [record](sdlc/changes/pandas-smoke/validation.md): **12 passed** across three suites; reads, nullable typed round trips, append, replacement, filtering/aggregation, NULLs, empty results, and multiple Arrow batches. | Table-read APIs, bound query parameters, `chunksize`, TLS, Flight SQL, clusters, other platforms, and other versions were not validated. Replacement drops/recreates the table; writes autocommit without rollback. |
| NumPy and scikit-learn | [Tests](../python/tests/test_ml_ingest.py): **2 passed** on Redis OSS 8.6.2 with Search; query an Arrow result through Redis ADBC, convert to NumPy, then fit/predict a small scikit-learn decision tree. | Demonstrates a custom local Python pipeline only; no native connector, Flight SQL path, model persistence, or production-sized training workload was tested. |
| Additional local Python workflows | [Tests and record](sdlc/changes/candidate-test-runner/validation.md): **5 passed, zero skips** on Redis Software 8.6.2 with active verified 4x QPF: Jupyter notebook kernel execution, HF Dataset persistence/filtering (including empty results), Streamlit AppTest reruns, and SciPy sparse solve. | Custom local application paths. Datasets 5.1.0 requires `num_shards=1` for empty save/reload; browser interfaces, hosted runtimes, HF Hub/model downloads, and Flight SQL remain untested. |
| dlt append destination | [Tests](../python/tests/test_dlt.py) and [record](sdlc/changes/dlt-integration/validation.md): 12 dlt cases passed on earlier sources; public-base run **5 passed, 7 live cases skipped**. | No live public-base result. Append only; replace/merge rejected. Failed writes/checkpoint replay can duplicate rows; no automatic schema migration or restored source state from Redis. Hosted CI and release-wheel compatibility remain unverified by that record. |
| DB-API and Polars | [Polars and driver tests](sdlc/changes/polars-regression-rerun/validation.md): **8 passed**, including typed connection/URI reads, writes/appends, filtering, aggregation, NULLs, and empty-result schemas. An earlier clean-wheel smoke run recorded **4 passed**. | Latest rerun used a source build; wheels, other platforms, Cloud, cluster topologies, TLS handshakes, Flight SQL, and performance were not validated by that rerun. |
| Ray worker-side reader | [Tests](../python/tests/test_ray_datasource.py) and [record](sdlc/changes/ray-worker-164/validation.md): **17 passed**, including partition/type coverage, empty schemas, parameters, worker-local streaming, cleanup/cancellation, retry, and TLS certificate rejection. | Source build pinned to Ray 2.59.0 with a matching native library on each worker. Live unordered reads have no shared snapshot or stable retries. Multi-node, abrupt process death, other versions/platforms, Flight SQL, and release wheels remain unverified. |
| Prefect custom Python tasks | [Tests](../python/tests/test_prefect.py) and [record](sdlc/changes/prefect-integration/plan.md): **4 passed**, zero skips; local orchestration, Arrow values/types, dependencies, concurrency, retries, and SQL errors. | Custom tasks, not a packaged ADBC connector. Remote workers, Prefect Cloud, `prefect-redis`, Flight SQL, and other platforms remain untested. Writes autocommit; retries need idempotent writes to avoid duplicates. |
| Dask Redis adapter | [Suite](../python/tests/test_dask.py): **8 passed** on Redis OSS 8.6.2 with Search using the current source build. | This validates the repository's adapter, not Dask's generic ADBC support. Other platforms, large-scale clusters, and Flight SQL remain untested by this run. |
| Airflow TaskFlow and packaged hook | Earlier [TaskFlow record](sdlc/changes/airflow-smoke/validation.md): **2 passed**. [Hook record](sdlc/changes/airflow-get-records/validation.md): **8 passed** across five hook cases and installation/autocommit checks; query values, parameters, NULLs, empty/multiple results, writes, option forwarding, and transaction rejection. | Import `RedisAdbcHook` explicitly: it is not a registered provider, so `SQLExecuteQueryOperator` still selects the generic Arrow hook for an `adbc` connection. Use `autocommit=True` for inherited `insert_rows()`. Remote executors, Flight SQL, Airflow 2.x, and a live hook TLS handshake remain untested. |
| Dagster community resource and IO manager | [Record](sdlc/changes/dagster-adbc-resource/validation.md): **14 passed** (six community cases, eight IO-manager cases), including library/manifest discovery, ingestion/fetch, bound values, option forwarding, reconnect persistence, assets, and cleanup. | Use `autocommit=True` for ADBCResource. Other platforms, TLS, Redis Cluster API, and other versions remain untested by this run. Multi-dimensional partitions and atomic/transactional replacement are unsupported by the IO manager. |
| DuckDB via Query.Farm `adbc_scanner` | [Test](../python/tests/test_duckdb.py) and [verification record](sdlc/changes/duckdb-community/validation.md): reads, joins, ingestion, direct INSERT/CTAS, read-only enforcement, and transaction rejection. | Columnar's `adbc` extension, Flight SQL, older extension builds, and universal predicate pushdown. |
| Standalone DuckDB `redis_adbc` | [Native tests](../duckdb-extension/test/test_native.py): **16 passed** with DuckDB 1.5.6; matching CLI harness: **12 scenarios** on both 1.5.6 and the pinned DuckDB 2.0 registry source, checking 10,003 rows. [Record](sdlc/changes/duckdb-community/validation.md). | Registry publication for every engine/platform remains unverified; the current build targets DuckDB 2.0. Writes, attached catalogs, and bound query parameters are unsupported; this extension is a SELECT-only interface. |
| SQLAlchemy / Superset (included in the table) | Separate Superset environment: **3 passed**, including SQLAlchemy reflection and Superset dataset / SQL Lab paths. [Tests](../python/tests/test_superset.py). | Browser-rendered charts, broad application workflows, and transaction semantics beyond autocommit. |

The Python suite's skipped Superset test ran separately in its own environment.
Configured CI jobs are not evidence that every job has passed; consult the linked
records for platform limits and the workflows for reproducible commands. Historical
passes do not certify the current checkout or latest upstream releases.

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

The companion [dbt adapter project](https://github.com/redis/redis-dbt-adapter)
records separate end-to-end validation. Its [v1 guide](https://github.com/redis/redis-dbt-adapter/blob/main/v1/README.md#validation-and-testing)
describes full dbt workflows and the dbt adapter functional suite for Core
1.10–1.x. The [v2 validation record](https://github.com/redis/redis-dbt-adapter/blob/main/v2/VALIDATION.md)
reports macOS arm64 and Linux coverage with dbt-oss 2.0.8, Redis 8.6.2 and Redis
ADBC v0.0.16, using custom engine commit
`90178580bd6f3353d758fff770fdc2c58c985ad0`. The linked
[historical Linux CI result](https://github.com/redis/redis-dbt-adapter/blob/main/v2/VALIDATION.md)
records successful completion on October 21, 2026. Its recorded coverage includes smoke
and feature fixtures, trusted TLS, and the full taxi workflow: 82 successful
build results plus one configured source warning, and 82 successes for the empty
build. Python table models, snapshots, contracts, merge, microbatch and persisted
docs are included in the recorded scenarios. This is experimental custom-engine
support; the standard dbt v2 executable does not include Redis, static analysis
remains disabled, and these results do not establish Flight SQL compatibility or
support for every deployment. SQL grants remain unsupported in both adapters. The driver now provides
[materialized-view SQL](sql-101-105.md); complete adapter materialization runs
remain unverified.

The [differential harness](../go/pgdiff/README.md) replays captured dbt-postgres SQL
through Redis ADBC. It does not validate a Redis dbt adapter or a full dbt run,
snapshots, or documentation generation against Redis.

The related [Redis Arrow Flight server](https://github.com/redis/redis-apache-arrow-flight#development)
documents Python ADBC, Go, and JDBC client coverage. Those results do not establish
tests of each application against this driver's current checkout. Application
configuration, metadata discovery, generated SQL, type mappings, authentication,
and refresh behavior still require validation where applicable.

No integration suites were rerun for this refresh. The claims above cite historical
results. SQL syntax similarity alone does not establish JDBC, ODBC, or PostgreSQL
wire-protocol compatibility, or SQLAlchemy compatibility beyond the explicitly
tested dialect operations.

[flight-validation]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/README.md
[flight-summary]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/summary.md
[flight-runner]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/integrations/testing.md
[flight-core]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/core.log
[flight-workflows]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/workflows.log
[flight-models]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/models.log
[flight-orchestrators]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/orchestrators.log
[flight-spark]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/spark.log
[flight-airflow]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/airflow.log
[flight-documents]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/documents.log
[flight-rstudio]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/rstudio.log
[flight-tableau]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/tableau.log
[flight-metabase]: https://github.com/redis/redis-apache-arrow-flight/blob/e53c1f37fa99b6593d72d35088c1b403ac8b397d/docs/validation/2026-11-03-flight-integrations/reports/metabase.log
