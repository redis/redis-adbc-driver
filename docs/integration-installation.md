# Driver installation for all 51 integrations

This maps every row of the [current 51-tool inventory](data-engineering-adbc-support.md)
to an installation choice. Use the [shared setup](setup.md) and linked tool guides
for configuration, dependencies and read/write limits. A driver installation
supplies the SQL/Arrow client; custom task/model code and separate connectors keep
their documented scope. Redis with Search is installed separately.

## Per-tool installation

For Python rows, install the listed distribution(s) with `python -m pip install`
in the interpreter that executes the integration. Quoted extras such as
`'adbc-driver-redis[pandas]'` bundle the native library and selected Python
dependencies. Follow the linked guide’s version bounds and host constraints,
especially Superset, Beam, Mage, Ibis and MLflow; do not combine all tools in one
environment. Use Python 3.14 for current integrations; the package minimum is 3.12.

| # | Tool | Recommended driver/package route | Placement and limits |
|---|---|---|---|
| 1 | pandas | `adbc-driver-redis[pandas]` | Install in the process running pandas; the wheel bundles the native driver. [Guide](integrations/pandas.md). |
| 2 | NumPy | `adbc-driver-redis[dbapi]` + `numpy` | Convert fetched Arrow values to arrays; no separate NumPy driver. [Guide](integrations/numpy.md). |
| 3 | Apache Spark | `adbc-driver-redis[dbapi]` + the guide’s Spark dependencies | Install in each connecting Python worker; custom Arrow bridge, not a JDBC JAR. [Guide](integrations/spark.md). |
| 4 | Jupyter / JupyterLab | `adbc-driver-redis[dbapi]` + `jupyterlab ipykernel` | Install in the selected notebook kernel environment. [Guide](integrations/jupyter.md). |
| 5 | scikit-learn | `adbc-driver-redis[dbapi]` + `numpy scikit-learn` | Custom array/model input and output code; no database backend registered. [Guide](integrations/scikit-learn.md). |
| 6 | PyTorch | `adbc-driver-redis[dbapi]` + `numpy torch` | Use the framework’s CPU/GPU distribution for the host; driver wheels are independent of CUDA. [Guide](integrations/pytorch.md). |
| 7 | TensorFlow | `adbc-driver-redis[dbapi]` + `numpy tensorflow` | Install in training/inference processes; follow TensorFlow platform constraints. [Guide](integrations/tensorflow.md). |
| 8 | Apache Airflow | `adbc-driver-redis[airflow]` under Airflow constraints | Install in Airflow services and workers; use redis_adbc connections and autocommit=True for SQL operators. [Guide](integrations/airflow.md). |
| 9 | Polars | `adbc-driver-redis[polars]` | Use URI ADBC mode or an open Redis DB-API connection. [Guide](integrations/polars.md). |
| 10 | dbt | Separate Redis adapter + `adbc-driver-redis[dbapi]` for Core v1 | The adapter has its own repository/release; experimental v2 requires a custom engine and native driver. [Guide](integrations/dbt.md). |
| 11 | Apache Kafka / Kafka Connect | Separate Redis Kafka connector, or evaluate Flight SQL/JDBC | Install connector/JDBC JARs in every Connect worker; native ADBC wheels cannot serve as JARs. [Guide](data-engineering-adbc-support.md). |
| 12 | Apache Flink | Separate Redis Flink connector, or evaluate Flight SQL/JDBC | Install Java connector artifacts in the Flink runtime; separate connector release ownership. [Guide](data-engineering-adbc-support.md). |
| 13 | Apache Beam | `adbc-driver-redis` + `apache-beam>=2.77,<3` | Use the guide’s compatible Arrow environment in every Python SDK worker; custom DoFn. [Guide](integrations/beam.md). |
| 14 | Trino | Separate Redis SQL Trino connector | Install the connector’s Java plugin; this ADBC library is not a Trino plugin. [Guide](data-engineering-adbc-support.md). |
| 15 | Dask | `adbc-driver-redis[dask]` | Install matching versions on scheduler/client and every worker that opens a connection. [Guide](integrations/dask.md). |
| 16 | Ray / Ray Data | `adbc-driver-redis[ray]` | Use a worker image/runtime environment with the wheel, matching Ray and private-CA configuration. [Guide](integrations/ray.md). |
| 17 | Apache NiFi | Evaluate Flight SQL JDBC with JDBC processors | Separate Flight SQL server and JDBC driver required; no packaged native ADBC processor. [Guide](data-engineering-adbc-support.md). |
| 18 | Airbyte | Build/register the separate proposed Redis ADBC source image | The source proposal pins its driver wheel; the Airbyte registry and image releases are separate. [Guide](data-engineering-adbc-support.md#airbyte-redis-adbc-source). |
| 19 | Fivetran | `adbc-driver-redis[dbapi]` + `fivetran-connector-sdk` | Include in a custom SDK source deployment; local harness coverage does not establish hosted deployment. [Guide](integrations/fivetran.md). |
| 20 | dlt | `adbc-driver-redis[dlt]` | Use the shipped custom destination; install in pipeline execution environments. [Guide](integrations/dlt.md). |
| 21 | SQLAlchemy | `adbc-driver-redis[sqlalchemy]` | Dialect entry points install with the wheel; use redis+adbc:// or rediss+adbc://. [Guide](integrations/sqlalchemy.md). |
| 22 | Apache Arrow PyArrow | `adbc-driver-redis[dbapi]` | Use the bundled driver through DB-API; no manifest needed for package imports. [Guide](integrations/pyarrow.md). |
| 23 | R / DBI | Managed native driver + CRAN `DBI`, `adbi`, `adbcdrivermanager` | Load the installed library in the R session; no Python package environment needed by R. [Guide](integrations/r-dbi.md). |
| 24 | SciPy | `adbc-driver-redis[dbapi]` + `numpy scipy` | Custom Arrow/NumPy handoff in the scientific application. [Guide](integrations/scipy.md). |
| 25 | XGBoost | `adbc-driver-redis[dbapi]` + `numpy xgboost` | Choose the host-compatible XGBoost build; install in training workers. [Guide](integrations/xgboost.md). |
| 26 | Hugging Face Transformers | `adbc-driver-redis[dbapi]` + `torch transformers tokenizers` | The model pipeline consumes application-prepared inputs; no Redis model backend is registered. [Guide](integrations/transformers.md). |
| 27 | MLflow | `adbc-driver-redis` + the guide’s MLflow/pandas/Arrow constraints | Use a separate compatible Arrow environment; custom data handoff, not an MLflow store plugin. [Guide](integrations/mlflow.md). |
| 28 | Streamlit | `adbc-driver-redis[dbapi]` + `streamlit pandas` | Install in the deployed Streamlit application environment. [Guide](integrations/streamlit.md). |
| 29 | Plotly | `adbc-driver-redis[dbapi]` + `plotly` | Query/convert data in the plotting process; no Plotly-specific driver binary. [Guide](integrations/plotly.md). |
| 30 | Matplotlib | `adbc-driver-redis[dbapi]` + `numpy matplotlib pillow` | Install in the rendering process; use the guide’s headless setup where needed. [Guide](integrations/matplotlib.md). |
| 31 | Tableau | Custom connector/Flight SQL JDBC candidate | No packaged Tableau ADBC connector supplied; installing this library does not register a Tableau source. [Guide](data-engineering-adbc-support.md). |
| 32 | Apache Superset | `adbc-driver-redis[superset]` in its own environment | Preserve the SQLAlchemy 1.4/Arrow compatibility bounds; install and restart every web/SQL Lab worker. [Guide](integrations/superset.md). |
| 33 | Metabase | Custom Java database driver/connector required | No packaged Metabase driver supplied; a wheel cannot become a Metabase JDBC plugin. [Guide](data-engineering-adbc-support.md). |
| 34 | Dagster | `adbc-driver-redis[dagster]` | Install in code locations and execution workers; add pandas/Polars only for those asset types. [Guide](integrations/dagster.md). |
| 35 | Prefect | `adbc-driver-redis[prefect]` | Install in task execution images; open connections inside task processes. [Guide](integrations/prefect.md). |
| 36 | Mage | `adbc-driver-redis` + the guide’s Mage-compatible dependencies | Keep Mage in its documented separate environment; custom loader/transformer blocks. [Guide](integrations/mage.md). |
| 37 | Kestra | Driver wheel + Python Script task dependencies from the guide | Install inside the task container/interpreter; the server’s Python environment is insufficient. [Guide](integrations/kestra.md). |
| 38 | Ibis | `adbc-driver-redis` + the guide’s Ibis/Arrow constraints | Custom Arrow memtable on a local backend; no Redis Ibis backend or automatic pushdown. [Guide](integrations/ibis.md). |
| 39 | Great Expectations | Custom supported SQL backend/dialect integration required | No packaged Redis data source supplied; SQLAlchemy availability alone does not establish compatibility. [Guide](data-engineering-adbc-support.md). |
| 40 | Soda | Custom Redis SQL data-source integration required | No packaged Soda Redis source supplied; a driver wheel alone does not add one. [Guide](data-engineering-adbc-support.md). |
| 41 | Microsoft Power BI | Custom Power Query connector/gateway required | No Windows driver binary or Power BI connector supplied; Flight SQL still requires connector work. [Guide](data-engineering-adbc-support.md). |
| 42 | Keras | `adbc-driver-redis[dbapi]` + `numpy tensorflow keras` | Use the guide’s TensorFlow backend environment; application code prepares model inputs. [Guide](integrations/keras.md). |
| 43 | Kubeflow Pipelines | `adbc-driver-redis[dbapi]` + `kfp` in component images | Install in each connecting component; local test evidence does not establish Kubernetes deployment. [Guide](integrations/kubeflow.md). |
| 44 | Metaflow | `adbc-driver-redis[dbapi]` + `metaflow` | Include in remote step environments as well as the local development interpreter. [Guide](integrations/metaflow.md). |
| 45 | Hugging Face Datasets | `adbc-driver-redis[dbapi]` + `datasets` | Custom Arrow-backed dataset conversion; preserve the guide’s empty-persistence handling. [Guide](integrations/huggingface-datasets.md). |
| 46 | Quarto | `adbc-driver-redis[dbapi]` + Python kernel dependencies; Quarto CLI separately | Install in the document execution kernel, including the build/publishing runner. [Guide](integrations/quarto.md). |
| 47 | Meltano | Custom tap/target or connector plugin required | No packaged Redis ADBC tap/target supplied. [Guide](data-engineering-adbc-support.md). |
| 48 | Apache Hop | Custom connection/transform plugin required | No packaged Hop ADBC connection supplied; native driver installation alone is insufficient. [Guide](data-engineering-adbc-support.md). |
| 49 | RStudio | Managed native driver + R/DBI packages, or the Python wheel in its selected interpreter | Install in the actual R session/Python interpreter; preserve the documented R binding limits. [Guide](integrations/rstudio.md). |
| 50 | Dataform | Custom target or external ingestion pipeline required | Standard Dataform workflows execute in BigQuery; no Redis target supplied. [Guide](data-engineering-adbc-support.md). |
| 51 | Sling | Official Sling CLI + native Redis ADBC library | Use an explicit driver path, isolated metadata template overrides, and a precreated Redis table with direct truncate-and-load. No Python extra required. [Guide](integrations/sling.md). |

## Native clients, R and DuckDB

For R and non-Python ADBC clients, prefer the existing
[managed native installer](native-installation.md). It verifies downloads and
native loading, maintains stable paths and ownership records, and supports
install, upgrade and uninstall. Keep these lifecycle protections when upgrading.
Future releases automatically attach this same installer and its checksum:

```sh
# Download install_driver.py and its .sha256 from the chosen GitHub release,
# verify the checksum, then run with Python 3.12+:
python3 install_driver.py install --version v<version>
python3 install_driver.py upgrade --version v<next-version>
```

Python applications should use wheels instead. Add `--only-binary=:all:` and pin
`adbc-driver-redis[<extra>]==<version>` in deployments to fail on missing native
wheels. Published driver targets remain Linux amd64/arm64 with glibc 2.28+ and
macOS arm64 12+. There are no Windows, macOS Intel or musl Linux driver wheels.
Install manifests only for clients that discover libraries by driver name; Python
package imports locate their bundled library automatically.

DuckDB is documented separately from the tool comparison because the inventory
excludes storage products. For catalogs and writes, use the native driver with
[adbc_scanner](duckdb.md). For streaming SELECT access, prefer the self-contained
`redis_adbc` community extension where a matching registry build is available.
The community submission has merged; publication is managed by the registry.

As a fallback, release CI now packages the existing tested extension for DuckDB
**2.0.0**, Linux amd64/arm64 and macOS arm64, named
`redis_adbc-<tag>-duckdb-v2.0.0-<target>.tar.gz`. Verify the adjacent SHA-256 and
extract the matching version/platform; `DUCKDB_VERSION` and `DUCKDB_PLATFORM`
identify the exact build. These unsigned GitHub artifacts require an
unsigned-enabled DuckDB client (CLI: `duckdb -unsigned`):

```sql
INSTALL '/absolute/path/redis_adbc.duckdb_extension';
LOAD redis_adbc;
SELECT * FROM redis_adbc_scan('redis://localhost:6379/0', 'SELECT 1 AS one');
```

The extension includes its driver/Go runtime and needs no separate manifest or
wheel. Linux extension builds use Ubuntu 24.04; the native library’s glibc 2.28
promise does not apply to these extension binaries. Match the build runtime or
build from source. See [extension details](../duckdb-extension/README.md).

## What future releases automate

Publishing a GitHub release builds/tests the existing native libraries, CLI,
Python wheels, flat dbc archives/catalog and standalone DuckDB extension. Asset
upload waits for the current native/platform/wheel-client and DuckDB gates,
then attaches versioned files and the checksummed managed installer.

A successful `Release binaries` run triggers `publish-pypi.yml` automatically.
The publisher reads the completed release tag artifact, rejects PR/unrelated or
failed runs, skips draft/prerelease tags and rebuilds using Go test skips, and
validates the exact three-wheel set, checksums, package/version and native
architecture before OIDC upload. Existing stable-tag manual retries and
metadata-only post-releases remain available. Both the binary and PyPI workflows
must succeed before declaring a release complete. These workflow changes must
reach `main` before the post-build trigger is available.

Verified on 2026-11-01 in signed-in browser settings: PyPI already authorizes
`redis/redis-adbc-driver`, workflow `publish-pypi.yml`, environment `pypi`.
GitHub’s matching environment exists without reviewers, timers or deployment
branch restrictions. No remote permissions/settings were changed. PyPI publishing
stays in its dedicated workflow because
[reusable workflows cannot currently be registered as Trusted Publishers](https://docs.pypi.org/trusted-publishers/troubleshooting/#reusable-workflows-on-github).
The trigger uses [workflow_run](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_run).

The pipeline publishes this repository’s artifacts. The dbt adapter and proposed
Airbyte image have separate repositories/builds. JDBC/native-command connectors
and Flight SQL servers have separate owners. dbc packages/catalog are generated
on every release, but Foundry signing/CDN onboarding still follows the existing
[dbc handoff](dbc.md); this PR does not make `dbc install redis` available in the
default registry. Updating the DuckDB community descriptor/build publication is
also separate from attaching fallback GitHub binaries. No new 50-tool end-to-end
compatibility claim follows from these distribution changes.
