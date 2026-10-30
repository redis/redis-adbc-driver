# ADBC support across 50 popular data engineering and data science tools

For installation and diagnostics, start with [Choose your tool and connect](setup.md).


Support and testing status refreshed on **2026-10-30** against this repository
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
spark-redis, and its ✈️ identifies the separate Flight SQL candidate. Kafka and
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

| # | Tool | Redis connection solution | Main purpose | General ADBC support | Redis ADBC support | Redis Arrow Flight SQL support | Redis-built / maintained connector | Third-party-built connector / alternative |
|---|---|---|---|---|---|---|---|---|
| 1 | pandas | ✅ 🔌 ✈️ | DataFrame processing | **Yes, directly.** SQL read APIs accept ADBC connections through the Python driver manager. [Integration guide](https://arrow.apache.org/adbc/main/integrations.html). | Tested `read_sql` / `read_sql_query` and `DataFrame.to_sql` with an open Redis ADBC connection: nullable typed round trips, append, replacement, filtering/aggregation, empty reads, and multiple Arrow batches. Combined read/write suite: **12 passed**. [Evidence](sdlc/changes/pandas-smoke/validation.md). | Pass an open ADBC Flight SQL connection to read_sql; tool-level Redis integration untested. | None verified. | No dedicated Redis connector verified. Custom extraction can feed [pandas.DataFrame.from_records](https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.from_records.html). |
| 2 | NumPy | ✅ 🔌 ✈️ | Numerical computing | ADBC results can be converted to Arrow and NumPy arrays through Python clients; NumPy itself is not a database connector. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested a Redis ADBC Arrow query result converted to a NumPy array and checked numeric values; custom application code only. | Candidate through Flight SQL and Arrow-to-NumPy conversion; untested. | None verified. | No dedicated Redis connector; custom application code required. |
| 3 | Apache Spark | ✅ ✈️ 🧩 | Distributed processing | **Driver available for querying Spark.** Its built-in external database reader uses JDBC; consuming another ADBC driver requires integration work. [Driver list](https://arrow.apache.org/adbc/main/driver/index.html), [JDBC reader](https://spark.apache.org/docs/latest/sql-data-sources-jdbc.html). | Tested a custom Python path: native Redis ADBC fetch → Arrow → local Spark 4.3.0; typed rows, NULLs/Unicode, filters, SQL aggregation, and empty schemas. This is not a Spark database datasource. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate via Flight SQL JDBC and Spark JDBC reader; validate JVM setup, metadata, partitioning, and SQL. | [spark-redis](https://github.com/RedisLabs/spark-redis), RedisLabs: Redis data structures and Spark SQL; older documented Spark targets. Full Redis command / Query Engine coverage is not established for this connector. | None verified. |
| 4 | Jupyter / JupyterLab | ✅ 🔌 ✈️ | Interactive notebooks | Python and R notebook kernels can load their respective ADBC client libraries. [ADBC client libraries](https://arrow.apache.org/adbc/main/client_libraries.html). | Tested notebook execution in a separate Python kernel: stored rows, Arrow types, NULLs/Unicode, and saved notebook outputs. [Evidence](sdlc/changes/candidate-test-runner/validation.md). JupyterLab browser UI remains untested. | Candidate using the Flight SQL client; untested as notebook workflow. | None verified. | No dedicated Redis notebook connector verified. |
| 5 | scikit-learn | ✅ 🔌 ✈️ | Machine learning | Can consume data prepared by pandas, Polars, or NumPy; no native database connector. [Python Developers Survey](https://lp.jetbrains.com/python-developers-survey-2024/) tracks it among widely used ML libraries. | Tested a Redis ADBC Arrow result converted to NumPy arrays, then used to fit and predict with a small decision tree; custom pipeline only. | Candidate through Flight SQL results converted to a supported array or dataframe; untested. | None verified. | No dedicated Redis connector verified. |
| 6 | PyTorch | ✅ 🔌 ✈️ | Deep learning | Database access is typically implemented in application data pipelines; ADBC Python clients can supply Arrow batches for conversion. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested a custom Dataset/DataLoader and CPU model forward pass from fetched ADBC rows, with an explicit NULL mask. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate through Flight SQL and a custom Dataset/DataLoader; untested. | None verified. | No dedicated Redis connector verified. |
| 7 | TensorFlow | ✅ 🔌 ✈️ | Machine learning framework | Database access is typically implemented in input pipelines; ADBC Python clients can supply Arrow data for conversion. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested a custom `tf.data.Dataset` input pipeline and batched values/NULL mask from fetched ADBC rows. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate through Flight SQL and custom input pipeline; untested. | None verified. | No dedicated Redis connector verified. |
| 8 | Apache Airflow | ✅ 🔌 ✈️ 🤝 | Workflow orchestration | **Yes, through the Apache Arrow provider's `AdbcHook`.** Install the provider and database driver. [ADBC connection](https://airflow.apache.org/docs/apache-airflow-providers-apache-arrow/stable/connections/adbc.html). | Locally tested with Airflow 3.3.2 TaskFlow and Arrow provider 0.1.1: ingest, SQL aggregation and cleanup. The packaged `RedisAdbcHook` validates `get_records()` / `get_first()`, bound queries, and writes using autocommit and bundled-driver discovery. Import it explicitly inside a Python task; the generic Arrow provider 0.1.1 hook still rejects `get_records()` transaction mode. [Hook guide](python-integrations.md#redis-hook-for-get_records). [Setup and scope](python-integrations.md#apache-airflow). | Candidate through AdbcHook with the Flight SQL driver, or Python tasks; untested. | None verified. | [Apache Airflow Redis provider / RedisHook](https://airflow.apache.org/docs/apache-airflow-providers-redis/stable/_api/airflow/providers/redis/hooks/redis/index.html), Apache Airflow: get_conn() exposes a native Redis client for command calls. |
| 9 | Polars | ✅ 🔌 ✈️ | Fast DataFrame processing | **Yes, directly.** Database read/write APIs support ADBC connections. [Integration guide](https://arrow.apache.org/adbc/main/integrations.html). | **Locally validated, 2026-10-29:** connection/URI reads, typed write/append round trips, filtering and aggregation, NULLs, and empty-result schemas. The unchanged [Polars regression suite](../python/tests/test_polars.py) plus driver tests passed all 8 cases with warnings treated as errors on Redis Software 8.6.2 with verified 4x QPF. [Evidence and reproduction](sdlc/changes/polars-regression-rerun/validation.md). | Pass an open ADBC Flight SQL connection to read_database; tool-level Redis integration untested. | None verified. | No dedicated Redis connector verified. Custom extraction can feed [polars.from_dicts](https://docs.pola.rs/api/python/stable/reference/api/polars.from_dicts.html). |
| 10 | dbt | ✅ 🔌 | SQL transformations | **Yes, through adapters.** The Rust-based dbt v2 engine uses ADBC; dbt Core v1 can use it through a database-specific Python adapter. An ADBC driver alone does not register a database in either engine. [ADBC requirements](https://docs.getdbt.com/docs/dbt/adbc), [Adapter development](https://docs.getdbt.com/docs/contribute-dbt-adapters-v2?version=2). | Available: [dbt-redis-adbc for Core 1.10–1.x](https://github.com/alberttwong/redis-dbt-adapter-with-project/tree/main/v1/adapter) and an [experimental native v2 adapter](https://github.com/alberttwong/redis-dbt-adapter-with-project/tree/main/v2) compiled into the [custom engine fork](https://github.com/alberttwong/dbt). The standard v2 binary does not include Redis; install the native Redis driver separately. [Validation scope](https://github.com/alberttwong/redis-dbt-adapter-with-project/blob/main/v2/VALIDATION.md). The current native v1 adapter also passed a local build, model-test, and catalog smoke against this checkout; [native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Unverified. The existing adapters load the Redis driver directly; they do not establish Flight SQL compatibility. Adapter connection/configuration work and end-to-end validation remain necessary. | None verified. | Community [v1/v2 adapter project](https://github.com/alberttwong/redis-dbt-adapter-with-project), with v2 Rust implementation in the engine fork. Redis, Inc. maintenance or support is not established. |
| 11 | Apache Kafka / Kafka Connect | ✅ 🧩 | Event streaming and integration | **No standard ADBC connector verified.** The common Confluent database connector uses JDBC. [JDBC connector](https://docs.confluent.io/kafka-connectors/jdbc/10.8/overview.html). | Custom Kafka Connect connector required; the JDBC connector cannot directly load an ADBC library. | Candidate through a JDBC source connector plus Flight SQL JDBC; verify polling/incremental SQL and type mappings. Not CDC. | [Redis Kafka Connector](https://redis-field-engineering.github.io/redis-kafka-connect/), Redis Field Engineering: sinks and keys/Streams sources; full command API / Query Engine access not established. | Confluent JDBC connector is a Flight SQL candidate, not a verified unrestricted Redis command connector. |
| 12 | Apache Flink | ✅ 🧩 | Stream processing | **No built-in generic ADBC connector verified.** Its documented relational database connector uses JDBC. [JDBC connector](https://nightlies.apache.org/flink/flink-docs-stable/docs/connectors/table/jdbc/). | Custom source/sink integration required; untested. | Candidate through the JDBC connector plus Flight SQL JDBC; validate dialect, types, batching, and streaming semantics. | [Redis Flink connector](https://github.com/redis-field-engineering/redis-flink-connector-dist), Redis Field Engineering / Redis, Inc.: Streams sources and Streams/JSON/Hash sinks. Full command API / Query Engine access not established. | Apache Flink JDBC connector is a Flight SQL candidate; check dialect and runtime compatibility. |
| 13 | Apache Beam | ❌ | Batch and streaming pipelines | **No built-in generic ADBC I/O verified.** Custom code is possible; documented database I/O includes JDBC. [JDBC I/O](https://beam.apache.org/releases/pydoc/current/apache_beam.io.jdbc.html). | Custom Beam I/O required; untested. | Candidate through Java JdbcIO / Python JDBC I/O with Flight SQL JDBC; runner packaging and query/write behavior need validation. | None verified. | [Java RedisIO](https://beam.apache.org/releases/javadoc/current/org/apache/beam/sdk/io/redis/RedisIO.html), Apache Beam: key/value reads/writes and Streams writes. Full command API / Query Engine access not established. |
| 14 | Trino | ✅ 🧩 | Distributed SQL queries | **Driver available** for querying Trino; this is distinct from a Trino connector to an arbitrary ADBC source. [Driver list](https://arrow.apache.org/adbc/main/driver/index.html). | No generic Redis ADBC source connector verified; Trino would need connector work. | No built-in Redis Flight SQL connector verified; a custom Trino connector would be needed. | [Redis SQL Trino](https://github.com/redis-field-engineering/redis-sql-trino), built by Redis Field Engineering / Redis, Inc.: SQL over indexed HASH/JSON data with filters and aggregations pushed to the Query Engine. [Documentation](https://redis-field-engineering.github.io/redis-sql-trino/) explicitly describes FT.SEARCH / FT.AGGREGATE use and Redis Cluster configuration. This is separate from the Trino project's built-in Redis connector; unrestricted native command passthrough is not established. | [Built-in Trino Redis connector](https://trino.io/docs/current/connector/redis.html), Trino project: selected key/value reads, no Redis Cluster. This is a different connector from Redis SQL Trino. |
| 15 | Dask | ✅ 🔌 | Parallel DataFrames and Python computation | **Via this repository's Redis adapter; no built-in generic ADBC reader verified.** Dask SQL readers use SQLAlchemy URIs; custom readers can use from_map. [SQL integration](https://docs.dask.org/en/stable/dataframe-sql.html), [Custom loading](https://docs.dask.org/en/latest/generated/dask.dataframe.from_map.html). | [adbc_driver_redis.dask.read_query](python-integrations.md#dask) provides lazy partitioned reads with worker-local ADBC connections and pandas Arrow dtypes. [Recorded local validation](sdlc/changes/dask-353/validation.md) includes two worker processes; caller supplies partition parameters and exact schema. | Custom from_map loading with adbc_driver_flightsql and pandas partitions; untested. The bundled Redis helper does not establish Flight SQL compatibility. | None verified. | This repository's [Dask adapter](../python/adbc_driver_redis/dask.py) supplies the ADBC path; Redis, Inc. authorship or support is not established. Native Redis command access is not claimed. |
| 16 | Ray / Ray Data | ✅ 🔌 | Distributed Python and ML data processing | **Via this repository’s Redis adapter; no built-in generic ADBC reader claimed.** Ray’s documented Datasource/ReadTask interface supports worker-side Arrow blocks. [Extension API](https://docs.ray.io/en/latest/data/api/doc/ray.data.Datasource.html). | [adbc_driver_redis.ray.read_query](python-integrations.md#worker-side-arrow-reader) plans live ADBC partitions and streams Arrow batches from worker-local connections. Source build with the ray extra, pinned to Ray 2.59.0; matching native library required on every worker. [Local validation](sdlc/changes/ray-worker-164/validation.md). | Unverified. This adapter loads the Redis driver directly; it does not establish Flight SQL compatibility. | None verified. | This repository’s [Ray adapter](../python/adbc_driver_redis/ray.py) supplies the ADBC path; Redis, Inc. authorship/support and upstream Ray endorsement are not established. Live unordered reads; no shared snapshot or stable retries. |
| 17 | Apache NiFi | ❌ | Visual data flows | **No standard ADBC processor verified.** Its standard SQL processor uses JDBC. [ExecuteSQL](https://nifi.apache.org/components/org.apache.nifi.processors.standard.ExecuteSQL/). | Custom processor or script integration required; untested. | Candidate through JDBC processors and Flight SQL JDBC; validate JVM options, metadata, and result conversion. | None verified. | [Redis connection/cache services](https://nifi.apache.org/components/org.apache.nifi.redis.service.RedisDistributedMapCacheClientService/), Apache NiFi: Redis-backed caching. Full command API / Query Engine access not established. |
| 18 | Airbyte | ❌ | Data replication | **No generic ADBC connector verified.** An ADBC driver alone does not automatically become an Airbyte connector. [Connector catalog](https://docs.airbyte.com/integrations), [ADBC feature request](https://github.com/airbytehq/airbyte/issues/24546). | Custom Airbyte connector required; untested. | Custom connector required; no generic Flight SQL connector verified. | None verified. | [Marketplace Redis destination](https://docs.airbyte.com/integrations/destinations/redis), Airbyte ecosystem: data-structure writes. Redis source and full command API / Query Engine access not verified. |
| 19 | Fivetran | ❌ | Managed data replication | **Custom integration candidate.** Its Python Connector SDK can use database libraries, but ADBC packaging and runtime compatibility need validation. [SDK guide](https://fivetran.com/docs/connector-sdk/getting-started). | Tested a custom native-ADBC source with Connector SDK 2.9.0’s real local tester and verified its DuckDB warehouse output. Hosted deployment and binary packaging remain unverified. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Custom Connector SDK source using the Python Flight SQL driver; untested. | None verified. | [Community Redis Connector SDK example](https://github.com/fivetran/community_connectors/tree/main/redis), Fivetran community: reads keyspace/TimeSeries data for replication. Does not expose unrestricted Redis API or Query Engine commands as a tool connector; custom SDK code could be extended. |
| 20 | dlt | ✅ 🔌 | Python data loading | **Yes, for selected destinations and custom integrations.** This repository supplies an ADBC ingestion destination. [Guide](python-integrations.md#dlt). | Tested the repository append-only `adbc_driver_redis.dlt.redis_adbc` destination: all 12 cases passed in the current native sweep on Redis Software with verified 4x QPF. Install the `dlt` extra; replace/merge remain unsupported and retries can duplicate rows. [Guide](python-integrations.md#dlt), [current evidence](sdlc/changes/candidate-test-runner/native-sweep.md). | No Flight SQL destination validated; custom Python resource loading remains untested. | None verified. | This repository's [dlt destination](../python/adbc_driver_redis/dlt.py) supplies the ADBC path; no Redis, Inc. maintenance or support claim. Append only; replace/merge are rejected, and retries can duplicate partially written rows. |
| 21 | SQLAlchemy | ✅ 🔌 ✈️ | Python SQL toolkit and ORM | External dialect projects include Redis via ADBC, but SQLAlchemy requires dialect support and the dialect's capabilities still need validation. [External dialect list](https://docs.sqlalchemy.org/en/20/dialects/). | Tested the repository dialect: engine queries, reflection, bound values, generated chart SQL, and transaction/savepoint rejection; 2 cases passed. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | A Flight SQL dialect/DBAPI pairing may work; no specific Redis Flight SQL SQLAlchemy dialect verified. | None verified. | [adbc-driver-redis](https://docs.sqlalchemy.org/en/20/dialects/) appears in SQLAlchemy's external dialect list; third-party status and maintenance should be checked. |
| 22 | Apache Arrow PyArrow | ✅ 🔌 ✈️ | Arrow data processing | PyArrow is the Arrow interchange layer used by Python ADBC clients; it provides Arrow arrays/tables and stream interfaces. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | 12 array/IPC/union-stream interface cases passed against the native driver, in addition to Arrow fetch/ingest across the application suites. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Used with Flight SQL results; driver integration still needs application code. | None verified. | No dedicated Redis connector verified. |
| 23 | R / DBI | ✅ 🔌 ✈️ | Statistical computing | R's `adbi` package provides a DBI-compliant interface backed by ADBC. [R DBI backends](https://r-dbi.org/backends/), [ADBC R docs](https://arrow.apache.org/adbc/main/r/). | Tested native-library loading through R 4.6.2 / adbi 0.1.3: typed reads, NULLs/Unicode, empty results, and cleanup. Bound parameters have a retained strict expected failure: adbi rejects unnamed parameter fields. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate through the R Flight SQL driver; validate package availability and types. | None verified. | No dedicated Redis DBI backend verified. |
| 24 | SciPy | ✅ 🔌 ✈️ | Scientific computing | ADBC can supply Arrow data that Python code converts to NumPy/SciPy structures; SciPy is not a database connector. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested fetched sparse-matrix coefficients, CSR construction, and a linear solve with checked solution/residual; custom local pipeline. [Evidence](sdlc/changes/candidate-test-runner/validation.md). | Candidate via Flight SQL and application conversion; untested. | None verified. | No dedicated Redis connector verified. |
| 25 | XGBoost | ✅ 🔌 ✈️ | Gradient-boosted trees | Training data is normally provided as arrays, dataframes, or matrices; ADBC can supply data through Python first. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested native ADBC rows → NumPy/DMatrix, tiny CPU training with a missing value, prediction checks, and model save/reload. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate via Flight SQL query then conversion; untested. | None verified. | No dedicated Redis connector verified. |
| 26 | Hugging Face Transformers | ✅ 🔌 ✈️ | Machine learning and language models | Python applications can fetch data through ADBC and convert it for model pipelines; Transformers itself is not a database connector. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested a local tokenizer and tiny randomly initialized BERT model from fetched labels, then offline model save/reload. No pretrained model/Hub download or training-scale validation. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate through Flight SQL and custom Python data preparation; untested. | None verified. | No dedicated Redis Transformers connector verified. |
| 27 | MLflow | ❌ | ML lifecycle management | Tracking and artifact storage use MLflow-specific backends; no generic ADBC database connector verified. [Backend stores](https://mlflow.org/docs/latest/self-hosting/architecture/backend-store/). | No driver loading path verified. | No Flight SQL integration verified. | None verified. | No qualifying Redis connector verified. |
| 28 | Streamlit | ✅ 🔌 ✈️ | Data apps and dashboards | Python app code can use ADBC directly; Streamlit does not provide a database-specific ADBC adapter. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested an AppTest app displaying stored Arrow rows/metrics and a bound-filter input rerun with NULLs/Unicode. [Evidence](sdlc/changes/candidate-test-runner/validation.md). Browser/hosting remains untested. | Candidate in app code using Flight SQL; untested. | None verified. | Custom Redis SDK code is possible but is not a packaged Streamlit connector. |
| 29 | Plotly | ✅ 🔌 ✈️ | Interactive visualization | Python application code can use ADBC to fetch data before plotting; Plotly is not a database connector. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested fetched rows with NULLs/Unicode → figure JSON save/reload and self-contained interactive HTML serialization; browser interaction untested. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate through application code; untested. | None verified. | No dedicated Redis connector verified. |
| 30 | Matplotlib | ✅ 🔌 ✈️ | Plotting and visualization | Can plot data fetched by Python ADBC clients; no database connection layer of its own. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested fetched rows → Agg rendering, checked plotted numeric values/NULL gap, and verified a nonblank PNG. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate through application code; untested. | None verified. | No dedicated Redis connector verified. |
| 31 | Tableau | ❌ | BI and visualization | No generic ADBC connector verified; database connectivity relies on supported connectors and its extension APIs. [Tableau connectors](https://www.tableau.com/products/connectors). | No Redis ADBC loading path verified in Tableau. | No native Flight SQL connector verified. | None verified. | No qualifying Redis connector verified. |
| 32 | Apache Superset | ❌ | BI and data exploration | Database connections use SQLAlchemy engines/dialects; not a generic ADBC consumer. [Superset database connections](https://superset.apache.org/docs/configuration/databases/). | Tested the repository SQLAlchemy dialect/engine spec with real dataset metadata, chart aggregation/time-series SQL, and SQL Lab execution in a temporary metadata database; 1 application test passed. Browser dashboards and hosting remain untested. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Flight SQL JDBC/ADBC is not a direct Superset SQLAlchemy backend. | None verified. | No Superset Redis connector meeting the guide's criteria verified. |
| 33 | Metabase | ❌ | BI and analytics | Database support is provided through built-in or community database drivers, not generic ADBC loading. [Metabase database drivers](https://www.metabase.com/docs/latest/databases/connections/). | A Metabase database driver is required; no Redis SQL driver verified. | No built-in Flight SQL connection verified. | None verified. | No qualifying Redis connector verified. |
| 34 | Dagster | ✅ 🔌 ✈️ | Data orchestration | **Yes, through a community integration.** `dagster-adbc` provides `ADBCResource`. [Integration guide](https://docs.dagster.io/integrations/libraries/adbc). | **Tested repository integration:** `adbc_driver_redis.dagster.RedisADBCIOManager` supports Arrow, pandas, Polars, and reader inputs/outputs, including partition tests. Install the `dagster` extra; [guide](python-integrations.md#dagster). The community `dagster-adbc` 0.0.2 `ADBCResource` is also locally validated with manifest or library-path discovery and explicit `autocommit=True`; [evidence](sdlc/changes/dagster-adbc-resource/validation.md). | Candidate through ADBCResource with driver="flightsql"; untested. | None verified. | Community dagster-adbc supplies generic ADBC integration. This repository's Redis-specific IO manager is listed in the Redis ADBC column; its presence does not establish Redis, Inc. maintenance. No separate native-command connector verified. |
| 35 | Prefect | ✅ 🔌 🤝 | Python workflow orchestration | **Custom Python integration.** Tasks can call ADBC libraries; a dedicated ADBC connector was not verified. [Python task API](https://reference.prefect.io/prefect/tasks/). | **Tested with Prefect 3.9.0 and Redis Software 8.6.2:** local flows, Arrow ingestion/fetch, dependencies, concurrent queries, retries, and SQL errors. [Usage](python-integrations.md#prefect-custom-python-tasks), [test evidence](sdlc/changes/prefect-integration/plan.md). | Custom Python tasks using adbc_driver_flightsql; untested. | None verified. | [prefect-redis](https://docs.prefect.io/integrations/prefect-redis), Prefect: RedisDatabase, locks, and messaging. Its [RedisDatabase.get_client()](https://reference.prefect.io/prefect_redis/blocks/#prefect_redis.blocks.RedisDatabase.get_client) exposes a native Redis client for direct command calls; this tool-specific integration is distinct from installing redis-py alone. |
| 36 | Mage | ❌ | Data pipeline authoring | Python blocks can invoke ADBC, but no generic ADBC connector is verified. [Mage integrations](https://docs.mage.ai/integrations). | Custom block/resource required; untested. | Custom block/resource required; untested. | None verified. | No qualifying Redis connector verified. |
| 37 | Kestra | ❌ | Workflow orchestration | ADBC driver loading is not a standard generic Kestra database plugin path. [Kestra plugins](https://kestra.io/plugins). | Custom task/plugin required; untested. | Custom Flight SQL task/plugin required; untested. | None verified. | [Kestra Redis plugin](https://kestra.io/plugins/plugin-redis) supports Redis data structures and pub/sub; it does not establish general SQL/Query Engine access. |
| 38 | Ibis | ❌ | Python analytics API | Ibis currently describes ADBC as an ecosystem topic, not a backend connection API. [Ibis ecosystem](https://ibis-project.org/concepts/composable-ecosystem). | No Redis Ibis backend verified; custom backend required. | No Redis Flight SQL backend verified. | None verified. | No qualifying Redis connector verified. |
| 39 | Great Expectations | ❌ | Data quality | Database validation generally depends on supported SQLAlchemy connections and execution engines, not generic ADBC. [GX data sources](https://docs.greatexpectations.io/docs/core/connect_to_data/sql_data/). | Requires supported SQL backend/dialect integration; untested. | No native Flight SQL path verified. | None verified. | No qualifying Redis connector verified. |
| 40 | Soda | ❌ | Data quality | SQL data sources use Soda's supported data source integrations; generic ADBC consumption not verified. [Soda data sources](https://docs.soda.io/soda-cl/data-sources.html). | Requires a Redis SQL data source implementation; untested. | No direct Flight SQL path verified. | None verified. | No qualifying Redis connector verified. |
| 41 | Microsoft Power BI | ❌ | BI and visualization | Power Query documents custom ADBC connectors; a Redis connector still needs to be developed and validated. [ADBC connector development](https://learn.microsoft.com/en-us/power-query/adbc). | No packaged Redis ADBC connector verified. | Custom Flight SQL connector candidate; untested. | None verified. | [CData Redis Power BI connector](https://www.cdata.com/kb/tech/redis-powerbi-setup.rst) is commercial and ODBC-based; full command / Query Engine access not established. |
| 42 | Keras | ✅ 🔌 ✈️ | Deep learning API | Training inputs can be supplied by application code using Python ADBC and Arrow; Keras is not a database connector. [ADBC Python clients](https://arrow.apache.org/adbc/main/python/quickstart.html). | Tested fetched native ADBC values as CPU model inputs, explicit missing-value handling, checked predictions, and `.keras` save/reload using the TensorFlow backend. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate via Flight SQL query and custom input pipeline; untested. | None verified. | No dedicated Redis connector verified. |
| 43 | Kubeflow Pipelines | ✅ 🔌 ✈️ | Machine learning workflow orchestration | Python pipeline components can load ADBC clients, but Kubeflow does not supply a generic ADBC connector. [Kubeflow Pipelines overview](https://www.kubeflow.org/docs/components/pipelines/overview/). | Tested a custom component with KFP 2.18.0’s local SubprocessRunner: task-local native ADBC query, typed/NULL/Unicode assertions, and checked materialized output. Kubernetes/remote execution untested. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate through a custom Python component using Flight SQL; untested. | None verified. | No dedicated Redis Kubeflow connector verified. |
| 44 | Metaflow | ✅ 🔌 ✈️ | Data science and ML workflow framework | Python tasks can use ADBC libraries; Metaflow itself does not provide a generic ADBC database connector. [Metaflow documentation](https://docs.metaflow.org/). | Tested a custom local flow with native ADBC reads, persisted rows between steps, and checked NULL/Unicode values. Remote backends untested. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate through a custom task using Flight SQL; untested. | None verified. | No dedicated Redis Metaflow connector verified. |
| 45 | Hugging Face Datasets | ✅ 🔌 ✈️ | Dataset loading and preparation | The library uses Arrow tables and can bridge ADBC query results into model datasets through custom Python code. [Arrow integration](https://huggingface.co/docs/datasets/use_with_pyarrow). | Tested Arrow conversion, local save/reload/filter, types/NULLs/Unicode, and empty results. Datasets 5.1.0 needs `num_shards=1` for empty persistence. [Evidence](sdlc/changes/candidate-test-runner/validation.md). | Candidate through Flight SQL and Arrow table conversion; untested. | None verified. | No dedicated Redis Datasets connector verified. |
| 46 | Quarto | ✅ 🔌 ✈️ | Scientific and technical publishing | Quarto runs code in Python and R documents; those code cells can use ADBC clients, but Quarto is not a database connector. [Quarto FAQ](https://quarto.org/docs/faq/). | Tested a Python document rendered to HTML by Quarto 1.10.20 with native ADBC queries and checked stored rows/NULLs/Unicode. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate through Python or R Flight SQL code; untested. | None verified. | No dedicated Redis Quarto connector verified. |
| 47 | Meltano | ❌ | ELT and data integration | Meltano orchestrates extractor and loader plugins; no generic ADBC connector verified. [Meltano integration guide](https://docs.meltano.com/guide/integration). | Custom tap/target or plugin work required; no packaged Redis ADBC connector verified. | Custom connector required; no generic Flight SQL plugin verified. | None verified. | No qualifying Redis ADBC or full-command connector verified. |
| 48 | Apache Hop | ❌ | Visual data integration and orchestration | Hop builds and runs pipelines through its plugin ecosystem; no generic ADBC connector verified. [Apache Hop overview](https://hop.apache.org/manual/latest/getting-started/hop-what-is-hop.html). | Custom transform/connection plugin required; untested. | Custom Flight SQL integration required; untested. | None verified. | No qualifying Redis connector verified. |
| 49 | RStudio | ✅ 🔌 ✈️ | R and Python development environment | RStudio IDE supports R and Python code; ADBC clients can be used from the corresponding language environment. [RStudio IDE guide](https://docs.posit.co/ide/user/index.html), [ADBC client libraries](https://arrow.apache.org/adbc/main/client_libraries.html). | Manually executed the saved native R/DBI harness in RStudio 2026.10.1 with R 4.6.2, including fixture creation, typed reads, empty results, and cleanup. Console success was verified; wider IDE workflows and the R binding limitation remain. [Native sweep](sdlc/changes/candidate-test-runner/native-sweep.md). | Candidate from an R or Python session using Flight SQL; untested as IDE integration. | None verified. | No dedicated Redis RStudio connector verified. |
| 50 | Dataform | ❌ | SQL transformation workflows | Dataform develops and schedules SQL workflows for BigQuery; no generic ADBC driver consumer verified. [Dataform overview](https://docs.cloud.google.com/dataform/docs/overview). | Redis adapter/target would be required; Dataform standard workflows execute in BigQuery. | No direct Flight SQL integration verified. | None verified. | No Redis Dataform adapter verified. |

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
(macOS 12+). Non-Python driver consumers still need a manifest or library path.
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
Fivetran would require a custom connector and validation of driver deployment.

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
outside this 22-tool selection. Their recorded checks cover reflection, bound SQL,
Superset discovery, dataset queries, and the SQL Lab cursor path; they do not
establish browser-rendered chart coverage or transactional ORM support.

## Testing status

The latest direct-driver sweep completed on **2026-10-30**: **124 passed, one
TLS-fixture skip, and one strict expected R/DBI binding incompatibility**, plus
a manual RStudio session check. All database runs used Redis Software 8.6.2
with verified active 4x QPF. The [complete 50-row accounting and environment
record](sdlc/changes/candidate-test-runner/native-sweep.md) distinguishes the
32 entries with exercised native paths from rows that still need an adapter or
connector. Flight SQL was excluded from this sweep at the user's request.

These are **recorded results from the linked PRs**, not integration suites rerun
for this documentation refresh. The latest merged evidence is:

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

No integration suites were rerun for this refresh. The claims above cite historical
results. SQL syntax similarity alone does not establish JDBC, ODBC, or PostgreSQL
wire-protocol compatibility, or SQLAlchemy compatibility beyond the explicitly
tested dialect operations.
