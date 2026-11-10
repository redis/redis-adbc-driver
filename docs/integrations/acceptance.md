# Upstream acceptance suites

Audit baseline: **2026-11-01**, covering the original 50 tools. Sling was added
on **2026-11-10** with its separate standalone workflow disposition. The table
now covers all 51 rows in the [tool comparison](../data-engineering-adbc-support.md),
including all 39 [integration guides](README.md). The checked sources are linked per row below.
“Workflow” means project tests exist, but no reusable database acceptance entry
point was identified for our particular Arrow/custom-code integration. It does
not mean that the upstream project has no tests. “Needs adapter” means an
upstream test framework exists but the required Redis connector is absent here.

## Run the available suites

```sh
make -C go redis-up  # disposable authenticated TLS Software; verifies 4x QPF
make -C go acceptance                         # all four contracts, sequentially
make -C go acceptance ACCEPTANCE_SUITES='sqlalchemy r-dbi'
```

`uv` must be on PATH (or set `UV` to its executable). R/DBI additionally needs
`Rscript` and the [pinned R acceptance environment](../../r/README.md): set
`R_LIBS_USER` to a dedicated library and run `Rscript r/install_acceptance.R`.
It installs adbi 0.1.4 and nanoarrow 0.9.0 with explicit dependency fixes.
The runner installs Python dependencies
in isolated uv environments;
it does not install R packages or provision databases. Use a dedicated empty test
database: upstream suites create/drop tables, schemas, and other test objects.
SQLAlchemy's provisioning hooks can enumerate and drop database objects.

With an already prepared fixture and library:

```sh
source go/build/test.env
export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so" # .dylib on macOS
python3 python/scripts/run-acceptance.py --list
bash go/software-setup.sh --exec python3 "$PWD/python/scripts/run-acceptance.py" all --results /tmp/redis-acceptance-results
# The existing candidate entry point also accepts these suites:
python/scripts/run-candidate-tests.sh acceptance sqlalchemy r-dbi
```

The Make target and `software-setup.sh --exec` recheck noeviction and active 4x
QPF and pass `REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed` only after that
management-plane verification. When invoking the Python/candidate entry point
directly, preserve that verified declaration for retained-partition tests; the
Software proxy cannot expose `CONFIG GET maxmemory-policy` itself.

The **Upstream integration acceptance** GitHub Actions workflow exposes the same
four choices or `all`, creates its own Software fixture, and uploads the reports.
It is manually dispatched because these broader contracts have substantial
runtime and dependency requirements. Results cover the supported analytics
contract; skipped identity, index, uniqueness and transaction features do not
establish full relational database compatibility.
The regular PR helper job tests the runner's failure/reporting behavior.

| Suite selector | Contract actually executed |
|---|---|
| `sqlalchemy` | SQLAlchemy 2.1.6's external-dialect suite, with Redis capability requirements. Reflection fixtures omit unsupported SQL index DDL; unmarked identity cases receive the same requirement as neighboring upstream tests. Assertions remain unchanged. Includes reads, inserts, types, DDL and reflection in an isolated schema. |
| `r-dbi` | `DBItest::test_all()` against the Redis DBI adapter, patched adbi/nanoarrow and this native library, with testthat's JUnit reporter. Runs binding, typed writes and metadata; skips transaction semantics outside the autocommit contract and the R-package naming convention for this sourced helper. Separate assertions verify transaction rejection. Covers RStudio's DBI layer, not its UI. |
| `fivetran` | Existing real SDK `Connector.debug()` tester and DuckDB warehouse value checks, reused from `test_fivetran_sdk_local_tester`. Source replication only; not a Redis destination acceptance suite. |
| `adbc` | Existing `adbc-drivers-validation` contract and Redis extensions under `go/validation/tests`, through that project's dependency environment. |

dbt and Airbyte have acceptance tests in their dedicated projects and are intentionally
not rerun here. Their source links are in the audit below.

Each selected suite produces `<suite>.xml`, a sanitized `<suite>.log`, and an
entry in `summary.json`. Setup errors, nonzero subprocess exits, malformed/missing
reports and zero passing cases fail the runner. Passes, failures, errors,
skips and xfails are counted separately. A failed suite does not prevent later
suites from running. Reports are removed before reruns so old passes cannot be
reused. SQLAlchemy is pinned in the runner. R and the
other Python dependency ranges follow their configured package repositories,
except the explicitly pinned adbi and nanoarrow sources. Upstream expected
failures remain separate from passes and follow pytest's exit status; no local
xfails are added to mask a Redis regression.

## Per-tool audit

An upstream project's own regression tests are not automatically tests of Redis.
Existing local workflows and read/write guide checks remain in the candidate
harness for paths where no reusable backend contract applies. Connector tests
for native Redis clients, JDBC or Flight SQL do not validate this ADBC driver.

| # | Integration | Upstream suite/source inspected | Harness disposition |
|---|---|---|---|
| 1 | pandas | [SQL I/O regressions](https://github.com/pandas-dev/pandas/tree/main/pandas/tests/io) | Workflow: existing pandas read/write tests; upstream database fixtures require their own backend registration. |
| 2 | NumPy | [Array tests](https://github.com/numpy/numpy/tree/main/numpy/_core/tests) | Workflow: Arrow conversion and computed-output guide. No database backend implemented by NumPy. |
| 3 | Apache Spark | [SQL/data-source tests](https://github.com/apache/spark/tree/master/sql/core/src/test) | Workflow: existing Arrow-to-local-Spark test; no Redis ADBC Spark datasource to register in upstream tests. |
| 4 | Jupyter / JupyterLab | [nbclient tests](https://github.com/jupyter/nbclient/tree/main/tests) | Workflow: execute and inspect notebook through existing test; UI tests would not validate the database contract. |
| 5 | scikit-learn | [Estimator checks](https://github.com/scikit-learn/scikit-learn/blob/main/sklearn/utils/estimator_checks.py) | Workflow: existing fit/predict path. Estimator checks target new estimators; this integration supplies arrays, not an estimator. |
| 6 | PyTorch | [Project tests](https://github.com/pytorch/pytorch/tree/main/test) | Workflow: Dataset/DataLoader and model input/output. No Redis backend extension here. |
| 7 | TensorFlow | [Dataset tests](https://github.com/tensorflow/tensorflow/tree/master/tensorflow/python/data/kernel_tests) | Workflow: existing custom `tf.data` bridge; no TensorFlow database kernel supplied. |
| 8 | Apache Airflow | [Arrow provider tests](https://github.com/apache/airflow/tree/main/providers/apache/arrow/tests) | Workflow: existing real hook/TaskFlow tests; upstream provider tests do not automatically load our subclass. |
| 9 | Polars | [Database I/O tests](https://github.com/pola-rs/polars/tree/main/py-polars/tests/unit/io/database) | Workflow: existing connection/URI reads and write/append round trips; upstream fixtures target configured engines. |
| 10 | dbt | [Adapter contracts](https://github.com/redis/redis-dbt-adapter/tree/280b609ad54d5bfae166c6eaa5a704273fbefb5a/v1/adapter/tests/functional) | **Dedicated project**: full adapter functional suite already lives in redis/redis-dbt-adapter. Excluded from this runner at the requestor's direction; Core v1 and experimental v2 retain their own tests. |
| 11 | Apache Kafka / Kafka Connect | [Connect tests](https://github.com/apache/kafka/tree/trunk/connect/runtime/src/test) | Needs adapter: no ADBC Kafka connector here. Native Redis source/sink and JDBC tests exercise different connectors. |
| 12 | Apache Flink | [JDBC connector tests](https://github.com/apache/flink-connector-jdbc) | Needs adapter: no ADBC Flink source/sink here; cannot substitute a JDBC or Streams connector test. |
| 13 | Apache Beam | [I/O tests](https://github.com/apache/beam/tree/master/sdks/python/apache_beam/io) | Workflow: existing custom DoFn, not an upstream Redis ADBC I/O implementation. |
| 14 | Trino | [Shared connector query framework](https://github.com/trinodb/trino/tree/master/testing/trino-testing/src/main/java/io/trino/testing) | Needs adapter: Redis SQL Trino is a separate Query Engine connector, not a consumer of this driver's library. |
| 15 | Dask | [DataFrame I/O tests](https://github.com/dask/dask/tree/main/dask/dataframe/io/tests) | Workflow: existing partitioned worker reader; no published reusable ADBC backend contract identified. |
| 16 | Ray / Ray Data | [Data tests](https://github.com/ray-project/ray/tree/master/python/ray/data/tests) | Workflow: existing real worker/datasource/TLS tests; upstream datasource-specific tests do not register our reader automatically. |
| 17 | Apache NiFi | [Redis bundle](https://github.com/apache/nifi/tree/main/nifi-extension-bundles/nifi-redis-bundle) | Needs adapter: no ADBC processor here; cache-service tests are a different integration. |
| 18 | Airbyte | [CDK standard suite registration](https://github.com/alberttwong/airbyte/blob/5c20a3f6e218d27c3e2f889e3cec53dbf8449453/airbyte-integrations/connectors/source-redis-adbc/integration_tests/standard_tests.py) | **Dedicated project**: CDK standard suite and native regressions live with the source proposal. Excluded from this runner at the requestor's direction; live SAT bypasses remain explicit in that project. |
| 19 | Fivetran | [SDK tester](https://fivetran.com/docs/connector-sdk/working-with-connector-sdk) | **Integrated: `fivetran`**, reuse existing real local tester and stored-value assertions. |
| 20 | dlt | [Destination load tests](https://github.com/dlt-hub/dlt/tree/devel/tests/load) | Workflow: existing custom resource/append destination tests. SQL destination contracts assume a fuller destination implementation than this callable. |
| 21 | SQLAlchemy | [External-dialect compliance protocol](https://github.com/sqlalchemy/sqlalchemy/blob/main/README.dialects.rst) | **Integrated: `sqlalchemy`**, entire shipped dialect suite in an independent pytest process. |
| 22 | Apache Arrow PyArrow | [ADBC validation](https://github.com/adbc-drivers/validation) | **Integrated: `adbc`**, existing driver contract plus Arrow interoperability/regression tests. PyArrow's own general tests are not driver acceptance. |
| 23 | R / DBI | [DBItest](https://dbitest.r-dbi.org/) | **Integrated: `r-dbi`**, supported DBI contract, including binding/type fixes and explicit transaction exclusions/rejection checks. |
| 24 | SciPy | [Sparse tests](https://github.com/scipy/scipy/tree/main/scipy/sparse/tests) | Workflow: existing matrix conversion/solver, not a database plugin. |
| 25 | XGBoost | [Project tests](https://github.com/dmlc/xgboost/tree/master/tests) | Workflow: existing CPU training/save/reload and guide output. |
| 26 | Hugging Face Transformers | [Project tests](https://github.com/huggingface/transformers/tree/main/tests) | Workflow: existing local tokenizer/model; no database backend implemented. |
| 27 | MLflow | [Project tests](https://github.com/mlflow/mlflow/tree/master/tests) | Workflow: dataset tracking; Redis is not an MLflow tracking store in this integration. |
| 28 | Streamlit | [App tests](https://github.com/streamlit/streamlit/tree/develop/lib/tests) | Workflow: existing AppTest path, not a reusable database backend suite. |
| 29 | Plotly | [Project tests](https://github.com/plotly/plotly.py/tree/main/tests) | Workflow: existing JSON/HTML artifact and stored-output checks. |
| 30 | Matplotlib | [Project tests](https://github.com/matplotlib/matplotlib/tree/main/lib/matplotlib/tests) | Workflow: existing Agg PNG and computed-output guide. |
| 31 | Tableau | [TDVT](https://github.com/tableau/connector-plugin-sdk/tree/master/tdvt) | Needs adapter: reusable validation exists, but no Redis Tableau connector/TDS/ODBC/JDBC endpoint supplied here. |
| 32 | Apache Superset | [Integration tests](https://github.com/apache/superset/tree/master/tests/integration_tests) | Existing isolated app/engine smoke test; SQLAlchemy compliance now adds the reusable database-layer contract. Superset app-wide tests require upstream app fixtures. |
| 33 | Metabase | [Driver tests](https://github.com/metabase/metabase/tree/master/test/metabase/driver) | Needs adapter: a Metabase driver and database fixture are required; none for this ADBC library here. |
| 34 | Dagster | [IO-manager integration tests](https://github.com/dagster-io/dagster/tree/master/python_modules/libraries/dagster-duckdb/dagster_duckdb_tests) | Workflow: existing Redis IO manager/resource/materialization/partition tests; upstream per-backend tests need adaptation. |
| 35 | Prefect | [Project tests](https://github.com/PrefectHQ/prefect/tree/main/tests) | Workflow: existing real tasks/flow; no Redis ADBC block protocol being implemented. |
| 36 | Mage | [Project tests](https://github.com/mage-ai/mage-ai/tree/master/mage_ai/tests) | Workflow: existing real loader/transformer pipeline; no upstream Redis ADBC connector class. |
| 37 | Kestra | [Project tests](https://github.com/kestra-io/kestra/tree/develop/tests) | Workflow: existing JVM Script-task run; no custom database plugin to register. |
| 38 | Ibis | [Backend conformance tests](https://github.com/ibis-project/ibis/tree/main/ibis/backends/tests) | Needs backend for those tests: our path uses Arrow `memtable` on DuckDB, not an Ibis Redis backend. Existing expression workflow remains. |
| 39 | Great Expectations | [Datasource documentation](https://docs.greatexpectations.io/docs/core/connect_to_data/) | Needs adapter/configuration: no Redis datasource contract verified; do not infer compatibility from a SQLAlchemy dialect. |
| 40 | Soda | [Project/backend tests](https://github.com/sodadata/soda-core) | Needs adapter: no Redis Soda datasource registered here. |
| 41 | Microsoft Power BI | [Power Query connector test framework](https://github.com/microsoft/DataConnectors/tree/master/testframework) | Needs adapter: no Power Query Redis connector/host integration supplied here. |
| 42 | Keras | [Testing helpers](https://github.com/keras-team/keras/tree/master/keras/src/testing) | Workflow: existing model input/persistence; integration does not implement a Keras backend. |
| 43 | Kubeflow Pipelines | [SDK tests](https://github.com/kubeflow/pipelines/tree/master/sdk/python/test) | Workflow: existing local component; cluster acceptance requires separate deployment and is not a database conformance suite. |
| 44 | Metaflow | [Flow tests](https://github.com/Netflix/metaflow/tree/master/test) | Workflow: existing local flow; remote executor acceptance is a separate concern. |
| 45 | Hugging Face Datasets | [Project tests](https://github.com/huggingface/datasets/tree/main/tests) | Workflow: existing Arrow conversion/save/reload/filter, not a database source implementation. |
| 46 | Quarto | [Render tests](https://github.com/quarto-dev/quarto-cli/tree/main/tests) | Workflow: existing executed/rendered document; not a reusable database contract. |
| 47 | Meltano | [Singer SDK tap/target test suites](https://github.com/meltano/sdk/tree/main/singer_sdk/testing) | Needs adapter: reusable suites require a Redis tap/target; neither is provided here. |
| 48 | Apache Hop | [Integration tests](https://github.com/apache/hop/tree/main/integration-tests) | Needs adapter: no Redis ADBC transform/connection plugin here. |
| 49 | RStudio | [IDE tests](https://github.com/rstudio/rstudio/tree/main/src/cpp/tests) | **DBI layer integrated: `r-dbi`**; IDE browser/desktop automation is separate. |
| 50 | Dataform | [Project tests](https://github.com/dataform-co/dataform/tree/main/tests) | Needs adapter: Dataform's warehouse workflows do not consume this Redis ADBC driver. |
| 51 | [Sling](https://slingdata.io) | [ADBC connector implementation](https://github.com/slingdata-io/sling-cli/blob/9d5186194e76e3e146b7d6367a6e27a961242dce/core/dbio/database/database_adbc.go) | Standalone workflow: [PostgreSQL round-trip test](../../scripts/test_sling_postgres.py), separate from this upstream acceptance runner. Two configured 10,003-row round trips passed with zero EXCEPT ALL differences; stock full-refresh and empty Arrow results failed. No reusable upstream Redis backend acceptance contract was integrated. [Evidence](../sdlc/changes/sling-postgres/validation.md). |

This audit assesses reuse of upstream test contracts for the implemented paths;
it does not certify tools listed as unverified in the comparison. Broader native
Redis connectors in other repositories retain their own test harnesses.
