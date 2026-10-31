# Redis ADBC driver

SQL queries and Arrow bulk ingestion over Redis through the Arrow Database
Connectivity (ADBC) API. This community driver bundles its native library in
platform wheels for Linux x86_64/arm64 (glibc 2.28+) and macOS arm64 (macOS 12+).
Use Python 3.14 for the current integrations. Python 3.12+ remains installable
for the separate Superset compatibility environment. Windows and macOS Intel wheels are not provided.

For installation and diagnostics, see [Choose your tool and connect](https://github.com/redis/redis-adbc-driver/blob/main/docs/setup.md).

```sh
python -m pip install 'adbc-driver-redis[dbapi]' polars
python -m adbc_driver_redis doctor --offline
```

**Recommended Redis configuration:** Redis Search (Query Engine) enabled
and query performance factor (QPF) set to **4×** where supported.
Search is required; 4× QPF is recommended for concurrent query performance.
Configure these on the Redis server; installing this package does not enable them.
See [server requirements](https://github.com/redis/redis-adbc-driver/blob/main/docs/driver-reference.md#server-requirements)
for configuration and CPU sizing guidance.

Set `REDIS_URI` and run `python -m adbc_driver_redis doctor` to check connectivity,
Search and a fetched `SELECT 1`. Add `--check-manifests` after registration for
native clients that load drivers by name.

With Redis and Search running:

```python
import polars as pl

df = pl.read_database_uri(
    query="SELECT 1 AS one",
    uri="redis://localhost:6379/0",
    engine="adbc",
)
print(df)
```

For direct DB-API use:

```python
from adbc_driver_redis import dbapi

with dbapi.connect("redis://localhost:6379/0") as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT 1 AS one")
        print(cur.fetch_arrow_table())
```

Use `rediss://` for TLS. Search is required; writes autocommit and transactions
are unsupported. SQL support comes from this driver. Installing the package
does not install or start a Redis server.

The `airflow` extra supports `adbc_driver_redis.airflow.RedisAdbcHook` for
autocommit queries through Airflow.

Optional extras: `sqlalchemy` for current SQLAlchemy, `superset` for the isolated
Superset 6.1 / SQLAlchemy 1.4 compatibility environment,
`dagster` for the Redis ADBC IO manager, and `dask` for partitioned DataFrame
reads with `adbc_driver_redis.dask.read_query`. The source build's `ray` extra
pins Ray 2.59.0 and supplies worker-side Arrow reads through
`adbc_driver_redis.ray.read_query`; see the
[Ray guide](https://github.com/redis/redis-adbc-driver/blob/main/docs/python-integrations.md#ray-data)
for installation on every worker and live-read consistency limits.

The `dlt` extra provides the append-only dlt destination:

```python
import dlt
from adbc_driver_redis.dlt import redis_adbc

pipeline = dlt.pipeline(
    pipeline_name="redis_orders",
    destination=redis_adbc(uri="redis://localhost:6379/0", schema_name="analytics"),
)
pipeline.run([{"id": 1, "amount": 10.5}], table_name="orders")
```

Install with `python -m pip install 'adbc-driver-redis[dlt]'`. Loads use Arrow
batches through ADBC. Only append is supported; retries can duplicate partially
written batches. Keep the local dlt pipeline directory for checkpoints.

See the [installation and integration guide](https://github.com/redis/redis-adbc-driver/blob/main/docs/python-integrations.md)
and [server requirements](https://github.com/redis/redis-adbc-driver/blob/main/docs/driver-reference.md#server-requirements)
for supported Redis deployments, configuration and limitations.

## Rerun candidate integration tests

The runners exercise direct Redis ADBC paths only. They start or reuse isolated
Redis Software with Search on `127.0.0.1:26379`, verify active 4x QPF through
`make -C go redis-up` before every group, and build the current native library.
See the [native sweep record](../docs/sdlc/changes/candidate-test-runner/native-sweep.md)
for every matrix entry's status, exact coverage, warnings, and compatibility limits.
The [five-tool follow-up](../docs/sdlc/changes/native-candidate-followup/validation.md)
adds Ibis, MLflow, Beam, Mage, and Kestra to that earlier coverage.

Run every automated native candidate in separate compatible environments:

```sh
python3 -m pip install uv
JAVA_HOME=/path/to/java21/Contents/Home KESTRA_JAVA=/path/to/java25/bin/java QUARTO=/path/to/quarto \
  python/scripts/run-all-native-candidates.sh
```

Prerequisites are Docker Compose, curl, jq, a Go compiler, Java 21 for Spark,
Java 25 or newer for Kestra 2.0.7,
R with a working package compiler, and Quarto CLI. On macOS, the Java/R/OpenMP
dependencies can be installed with `brew install openjdk@21 r libomp`.
Use the official Quarto installation for the document test. The script creates
Python 3.12/3.14 environments using uv and installs R DBI/ADBC packages in an
isolated library. The native dbt adapter is installed from its recorded immutable
source revision; set `CANDIDATE_DBT_ADAPTER` to use another source requirement.
DuckDB and Fivetran's local SDK tester may download official runtime artifacts.
The Kestra profile downloads the pinned official 2.0.7 executable, verifies its
published SHA-256, and installs the Python Script plugin 1.18.6 into the cache.
Set `KESTRA_JAR` and `KESTRA_PLUGINS_PATH` to reuse a prepared matching runtime.
Its server runs only for the test, on a loopback endpoint with generated local
authentication and temporary storage; it is stopped after the tests.

The profiles remain separate because Airflow/Prefect have incompatible FastAPI
requirements and Superset uses an older SQLAlchemy/PyArrow stack:

| Profile | Python | Dependency extras / tools |
|---|---|---|
| core | 3.12 | `test,candidate-tests`: pandas, Polars, NumPy/scikit-learn, SQLAlchemy, Arrow, DuckDB, Dask, Dagster, Prefect, dlt, Ray |
| workflows | 3.12 | `workflow-tests`: Jupyter, HF Datasets, Streamlit, SciPy |
| models | 3.12 | `plotting-tests,model-tests`: Plotly, Matplotlib, XGBoost, PyTorch, TensorFlow, Keras, Transformers |
| orchestrators | 3.12 | `orchestrator-tests`: local Kubeflow, Metaflow, Fivetran SDK |
| spark | 3.12 | `spark-tests`: fetched Arrow results into local Spark; Java required |
| airflow | 3.14 | `airflow-tests`: packaged hook and TaskFlow |
| superset | 3.12 | `superset,test-runner`: dataset/chart SQL and SQL Lab |
| dbt | 3.14 | `dbt-tests` plus native adapter: build, model tests, catalog |
| documents | 3.12 | `workflow-tests` plus R/Quarto: DBI reads/binding check and HTML render |
| analysis | 3.12 | `analysis-tests`: Ibis expressions and MLflow dataset tracking; PyArrow below 26 |
| beam | 3.12 | `beam-tests`: native ADBC reads inside local FnApi worker subprocesses |
| mage | 3.12 | `mage-tests`: real local loader/transformer pipeline |
| kestra | 3.12 | `kestra-tests`: native Python Script tasks, file handoff, checked task outputs; Java 25+ |
| ray-tls | 3.12 | `ray-tests`: owned private-CA Software fixture, remote worker reads and trust rejection |

Ibis, MLflow, Beam, Mage, and Kestra each cover populated and empty results.
These are custom native ADBC/Arrow/Python workflows. They do not establish an
Ibis Redis backend, an MLflow Redis tracking store, a packaged Beam I/O connector,
or hosted application support. MLflow 3.17.2's automatic digest fails on pandas
3.1.0 empty string columns; the empty workflow uses its public explicit-digest
option, while the exact default-path failure is retained as a strict expected
failure ([upstream bug](https://github.com/mlflow/mlflow/issues/27358)).

MLflow and Beam's released dependency caps require PyArrow below 26. Their
profiles retain compatible versions. MLflow's PyArrow 26 update already merged
in [#27216](https://github.com/mlflow/mlflow/pull/27216); Beam's request is
[#40919](https://github.com/apache/beam/issues/40919).

Run just this follow-up batch with the same saved launcher and isolated profiles:

```sh
CANDIDATE_PROFILES="analysis beam mage kestra" KESTRA_JAVA=/path/to/java25/bin/java \
  python/scripts/run-all-native-candidates.sh
```

Set `CANDIDATE_ENV_ROOT` to choose the environment/cache directory, `UV` to use
a specific uv executable, and `CANDIDATE_RESULTS_DIR` to choose the report
directory. Defaults are under the system temporary directory. Each profile
writes a log, JUnit XML, and launcher exit code; `summary.json` distinguishes
passes, failures, errors, skips, and expected failures. `CANDIDATE_PROFILES` selects
which profiles to run and summarize; omitted profiles are not represented as
current passes. A setup failure in one
profile still allows the other profiles to run. RStudio requires the separate
manual IDE check: source [r_dbi_smoke.R](examples/r_dbi_smoke.R) from an R session
with the same `REDIS_URI`, `REDIS_ADBC_LIBRARY`, and R library path.

For a focused run, install the corresponding extras in a virtual environment
and select tools by name. For example:

```sh
python3 -m venv .venv-workflows
.venv-workflows/bin/python -m pip install -e 'python[workflow-tests]'
PYTHON="$PWD/.venv-workflows/bin/python" python/scripts/run-candidate-tests.sh jupyter datasets streamlit scipy
```

Use `python/scripts/run-candidate-tests.sh --help` for tool names. Omitting
names or passing `all` delegates to the isolated all-native runner. The runner
leaves Redis Software available; remove it with
`COMPOSE_PROJECT_NAME=redis-adbc-candidates make -C go redis-down`.
`SOFTWARE_DB_PORT`, `SOFTWARE_API_PORT` (default `29443`), and
`CANDIDATE_COMPOSE_PROJECT` select another fixture. `REDIS_URI` is set to the
verified fixture. Flight SQL paths are excluded. Hosted deployment, GPU/model
scale, and browser UI coverage are not implied by these local tests.

### Ray workers with a private CA

Install `python[ray-tests]` and select the dedicated TLS profile:

```sh
PYTHON=/path/to/ray-venv/bin/python python/scripts/run-candidate-tests.sh ray-tls
```

This selector runs on its own. It creates a fresh Software project, verifies 4x
QPF, installs a short-lived private certificate on the Software proxy, and supplies
its authenticated `rediss://` endpoint and CA to the datasource tests. The worker
must preserve every row and Arrow schema with trust supplied and reject the
certificate when trust is removed. The profile fails if the TLS fixture is absent.
Trust is passed through `adbc.redis.tls_ca_file`; it is not installed globally or
used for package downloads. Containers, network and private material are removed
after both successful and failing runs.

`CANDIDATE_TLS_API_PORT`/`CANDIDATE_TLS_DB_PORT` default to `30443`/`27379`.
`CANDIDATE_TLS_COMPOSE_PROJECT` selects a project name; the default is unique to
the process. A pre-existing project is rejected. The all-native launcher runs
this as its own environment and keeps a separate `ray-tls` report. The ordinary
plaintext Ray group still reports its TLS-case skip explicitly.

### Dependency regressions

R binding tests cover single/multiple parameters, NULLs and rebinding, while
asserting the driver's unnamed parameter schema. adbi 0.1.3 still has strict
expected failures; its Arrow binding method also reaches the same name check.
A proposed upstream correction passes the tests in an isolated package install;
it is not a supported released dependency yet.

Datasets tests now exercise default saves independently and with fetched ADBC
results. Datasets 5.1.0's exact zero-shard empty reload error remains a strict
expected failure. An explicit empty-shard case preserves the tested mitigation;
nonempty results use defaults. Keep `num_shards=1` for empty results until a
released fix passes the default-sharding regression.

See the [follow-up validation](../docs/sdlc/changes/integration-252-254/validation.md)
for results, proposed upstream patches and remaining dependency work.

Arrow IPC stream/file serialization is available inside the native driver through
standard ADBC statement options. See the [IPC contract and example](https://github.com/redis/redis-adbc-driver/blob/main/docs/arrow-ipc.md).
