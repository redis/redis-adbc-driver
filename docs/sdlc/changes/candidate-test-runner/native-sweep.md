# Direct Redis ADBC candidate sweep

This is the original 32-entry sweep. The later
[five-tool follow-up](../native-candidate-followup/validation.md) extends native
coverage to 37 entries; retain the result counts below as historical evidence.

- Date: 2026-10-30
- PR: [#248](https://github.com/redis/redis-adbc-driver/pull/248)
- Scope: direct Redis ADBC only, as requested. No Flight SQL path was tested.
- Native driver source: base `22fee6883b9440c0d9999e9f967bc390bb0fd0e3`;
  this PR changes test harnesses/documentation, not the driver implementation.
- Test/runner source: this PR's native-sweep follow-up to `a99c882b4088aaff32e1abdad449f6e1b575b41a`.

## Result and reproduction

The complete saved launcher exited **0**: **124 passed, one skipped, one strict
expected failure** across nine isolated environments. RStudio also passed a
separate manual IDE-session check with the saved R script. All database runs
used Redis Software 8.6.2, one shard, with Search and active verified 4x QPF.
The repository's setup verifies effective workers/routing on both new and reused
fixtures before each group. [Machine-readable result counts](native-results.json).

The complete launcher installs compatible Python environments with uv, invokes
the native suites, and retains per-profile logs, JUnit XML, and launcher exit
codes. A dependency/setup failure is reported even if a test report contains
passing cases. The [README](../../../../python/README.md#rerun-candidate-integration-tests)
lists Java, R, Quarto, and other prerequisites.

```sh
UV=/path/to/uv JAVA_HOME=/path/to/jdk/Contents/Home QUARTO=/path/to/quarto \
  python/scripts/run-all-native-candidates.sh
```

| Environment profile | Passed | Skipped | Expected failures | Launcher exit |
|---|---:|---:|---:|---:|
| core | 97 | 1 | 0 | 0 |
| workflows | 5 | 0 | 0 | 0 |
| models/plotting | 7 | 0 | 0 | 0 |
| orchestrators | 3 | 0 | 0 | 0 |
| Spark | 1 | 0 | 0 | 0 |
| Airflow | 7 | 0 | 0 | 0 |
| Superset | 1 | 0 | 0 | 0 |
| dbt | 1 | 0 | 0 | 0 |
| R/Quarto documents | 2 | 0 | 1 | 0 |

## Tested environments

macOS arm64; Python 3.12.16 for core, models, workflows, orchestration, Spark,
Superset, and documents; Python 3.14.10 for Airflow and dbt. The main stacks use
ADBC driver manager 1.13.0 and PyArrow 26.0.0. Superset's separate compatibility
environment uses PyArrow 18.1.0, pandas 2.1.4, and SQLAlchemy 1.4.54.

| Application/library | Tested version |
|---|---|
| pandas / Polars / NumPy / SciPy | 3.1.0 / 2.1.0 / 2.5.4 / 1.18.1 |
| scikit-learn / XGBoost | 1.9.1 / 3.4.1 |
| PyTorch / TensorFlow / Keras / Transformers | 2.15.0 / 2.21.0 / 3.15.1 / 5.22.1 |
| Plotly / Matplotlib | 7.1.0 / 3.11.2 |
| nbclient / ipykernel / HF Datasets / Streamlit | 0.11.0 / 7.4.0 / 5.1.0 / 1.66.1 |
| Dask / Ray / DuckDB / SQLAlchemy | 2026.8.0 / 2.59.0 / 2.0.0 / 2.1.6 |
| Dagster / dagster-adbc / Prefect / dlt | 1.13.28 / 0.0.2 / 3.9.1 / 1.32.0 |
| Airflow / Apache Arrow provider | 3.3.2 / 0.1.1 |
| Kubeflow Pipelines / Metaflow / Fivetran Connector SDK | 2.18.0 / 2.19.40 / 2.9.0 |
| Spark / Java | 4.3.0 / OpenJDK 21.0.13.0.3 |
| Superset / dbt Core / dbt adapter | 6.1.0 / 1.12.6 / 0.1.0 |
| R / DBI / adbi / adbcdrivermanager | 4.6.2 / 1.3.0 / 0.1.3 / 0.25.0 |
| Quarto / RStudio Desktop | 1.10.20 / 2026.10.1 build 239 |

The dbt v1 adapter is pinned to source commit
`c9574b98762f4ff80b60a92b2d4137c2b03bf9df` in
[`redis/redis-dbt-adapter`](https://github.com/redis/redis-dbt-adapter/tree/c9574b98762f4ff80b60a92b2d4137c2b03bf9df/v1/adapter).
Its profile loads this checkout's native shared library, not a bundled older
driver. Java, R, OpenMP, and RStudio were installed from Homebrew; Quarto's official
archive was checksum-verified. Python dependencies and test artifacts are isolated
from the user's project environments.

## Compatibility limits and warnings retained

- Open follow-ups from this sweep: [R/DBI binding #252](https://github.com/redis/redis-adbc-driver/issues/252),
  [upstream Datasets persistence #253](https://github.com/redis/redis-adbc-driver/issues/253),
  and [Ray private-CA coverage #254](https://github.com/redis/redis-adbc-driver/issues/254).
  The Ray issue is a coverage gap, not a reproduced TLS defect. The Datasets
  issue is a local tracker for independently reproduced upstream behavior.
- R/DBI reads preserve numeric/boolean types, NULLs, Unicode, and empty schemas.
  **adbi 0.1.3's `dbBind()` rejects the driver's unnamed parameter schema** with
  `Expecting named params for named placeholders`. The binding case remains a
  strict expected failure scoped to that exact version/message; other failures
  are not converted to expected failures, and a successful binding forces an
  update to the test. The native driver advertises unnamed fields by design.
- One Ray datasource TLS rejection test skipped because it requires a private
  CA fixture. The ordinary direct read/write and worker/Arrow paths ran.
- The Ray stack emitted three deprecation warnings; Keras emitted two upstream
  NumPy `__array__` deprecation warnings. Spark emitted a warning that pandas 3.x
  is not fully supported. The warnings were not filtered or reclassified.
- HF Datasets 5.1.0 requires explicit `num_shards=1` for empty save/reload;
  the default-sharding failure reproduced independently of Redis. The earlier
  [workflow validation](validation.md) records it.
- Airflow 3.3.2 and Prefect 3.9.1 require incompatible FastAPI ranges. Superset's
  older SQLAlchemy/PyArrow versions also need isolation. The all-native launcher
  uses separate environments rather than forcing a single dependency set.
- DuckDB's signed Query.Farm repository can already be registered; its setup now
  uses `CREATE OR REPLACE`. The first setup error and focused successful rerun
  were followed by a complete successful native sweep.
- Fivetran used the real local tester and checked its DuckDB warehouse output.
  Only its artifact-cache directory was redirected; the source read, SDK
  operations, tester, and destination were not mocked. Hosted binary packaging
  and managed Fivetran execution were not tested.
- Spark consumed locally fetched Arrow data. This is a Python composition,
  not a Spark database datasource or JDBC test. Models are tiny CPU workflows;
  GPU support, pretrained model downloads, and training at scale were not tested.
- The RStudio check opened the IDE, sourced the saved
  [R harness](../../../../python/examples/r_dbi_smoke.R), and verified
  `REDIS_ADBC_R_DBI_OK` in the console and report file after cleanup. The test
  session was closed without saving a workspace. Broader UI flows were not tested.

## Accounting for all 50 comparison entries

The table below accounts for every row of the current comparison. **32 entries
have exercised native paths**, including the manual RStudio check. The remaining
rows do not have an established executable direct-ADBC integration in this
matrix: they require connector/adapter implementation, or their proposed path
uses Flight SQL and was excluded. They are not counted as passes or skipped tests.
This is not a claim that custom integrations for those tools are impossible.

| # | Entry | Native sweep result and scope |
|---|---|---|
| 1 | pandas | Passed: direct reads, nullable typed writes/appends/replacement, generated SQL; 12 application cases. |
| 2 | NumPy | Passed: fetched Arrow values converted to checked arrays. |
| 3 | Spark | Passed: local Arrow-to-Spark rows/types/NULLs, filter, aggregation, and empty schema. |
| 4 | Jupyter / JupyterLab | Passed: separate Python kernel execution and saved notebook results; browser UI untested. |
| 5 | scikit-learn | Passed: fetched data into a small estimator with checked predictions. |
| 6 | PyTorch | Passed: Dataset/DataLoader, explicit NULL mask/fill, and CPU forward pass. |
| 7 | TensorFlow | Passed: custom tf.data inputs/batches and checked values/mask. |
| 8 | Airflow | Passed: seven packaged-hook/TaskFlow cases in isolated Airflow 3.3.2. |
| 9 | Polars | Passed: four tool cases plus four shared driver cases. |
| 10 | dbt | Passed: native v1 adapter build, model tests, and catalog; experimental v2 engine not rerun. |
| 11 | Kafka / Kafka Connect | No generic direct ADBC connector established here; the proposed JDBC/Flight path is excluded. Redis Kafka connectors are a different interface. |
| 12 | Flink | Direct ADBC source/sink still requires integration code; JDBC/Flight candidate excluded. |
| 13 | Beam | Direct ADBC I/O still requires implementation; JDBC/Flight candidate excluded. |
| 14 | Trino | No generic ADBC source connector established; Redis SQL Trino is a separate Redis-native connector. |
| 15 | Dask | Passed: eight adapter cases including local distributed workers. |
| 16 | Ray Data | Passed: 22 cases across SQL, Arrow, and worker datasource paths; one private-CA case skipped. |
| 17 | NiFi | Direct ADBC processor still requires implementation; JDBC/Flight candidate excluded. |
| 18 | Airbyte | Custom source/connector implementation required; no generic ADBC loading path established here. |
| 19 | Fivetran | Passed locally: actual SDK sync and checked local warehouse rows; managed deployment untested. |
| 20 | dlt | Passed: all 12 append-destination cases with live Redis. |
| 21 | SQLAlchemy | Passed: dialect queries, reflection, generated SQL, and autocommit limitations. |
| 22 | PyArrow | Passed: array, IPC, and union-stream tests plus application Arrow fetch/ingest. |
| 23 | R / DBI | Reads passed; the exact adbi 0.1.3 binding incompatibility is retained as a strict expected failure. |
| 24 | SciPy | Passed: fetched sparse coefficients, checked solve and residual. |
| 25 | XGBoost | Passed: missing-value input, tiny training, predictions, model save/reload. |
| 26 | Transformers | Passed: local tokenizer, tiny BERT inference, offline model save/reload. |
| 27 | MLflow | No ADBC backend/loading path established by this matrix. |
| 28 | Streamlit | Passed: AppTest dataframe/metric and bound-filter rerun; browser UI untested. |
| 29 | Plotly | Passed: figure data/NULLs/Unicode, JSON save/reload, self-contained HTML. |
| 30 | Matplotlib | Passed: plotted values/NULL gap and nonblank Agg PNG output. |
| 31 | Tableau | No Redis ADBC connector/loading path established here. |
| 32 | Superset | Passed: dataset metadata, chart/time-series SQL, and SQL Lab in one application case. |
| 33 | Metabase | Requires a database driver; no direct Redis ADBC driver integration established here. |
| 34 | Dagster | Passed: eight IO-manager and six community ADBCResource cases. |
| 35 | Prefect | Passed: four local orchestration/dependency/concurrency/retry/error cases. |
| 36 | Mage | A custom block/resource is still required; no packaged direct-ADBC integration established in this comparison. |
| 37 | Kestra | A custom direct-ADBC task/plugin is still required; proposed Flight task excluded. |
| 38 | Ibis | Requires a Redis backend implementation; no direct ADBC backend established. |
| 39 | Great Expectations | Requires compatible SQL datasource/execution-engine integration; not an established direct ADBC consumer here. |
| 40 | Soda | Requires a Redis SQL data-source implementation; no generic ADBC consumer established. |
| 41 | Power BI | Requires a custom Redis connector; proposed Flight connector excluded. |
| 42 | Keras | Passed: model inputs, checked predictions, and native Keras persistence. |
| 43 | Kubeflow Pipelines | Passed: actual local component execution and checked output; Kubernetes deployment untested. |
| 44 | Metaflow | Passed: actual local flow and persisted values between steps. |
| 45 | HF Datasets | Passed: populated/empty Arrow conversions, checked features, local persistence/filtering. |
| 46 | Quarto | Passed: Python execution and checked generated HTML. |
| 47 | Meltano | Requires a custom tap/target/plugin; no generic Redis ADBC connector established. |
| 48 | Apache Hop | Requires a direct ADBC connection/transform plugin; no generic path established. |
| 49 | RStudio | Manual IDE check passed using the native R/DBI harness. |
| 50 | Dataform | Standard target is BigQuery; a Redis adapter/target would need implementation. |
