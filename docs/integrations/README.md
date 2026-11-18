# Redis integration guides

The standard way to integrate your application with Redis is to use the
[official Redis clients and libraries](https://redis.io/docs/latest/develop/clients/)
for your programming language. Redis ADBC is another option for workflows that
benefit from SQL and Apache Arrow, including the data-tool integrations described here.

This index collects the integration guides linked from the README, including
the dedicated DuckDB guide.
The [shared example setup](example-setup.md) supports the additional local workflows.
The 36 guides with a bidirectional native or custom ADBC path now verify tool
outputs written back to Redis as well as reads. See the
[read/write coverage and rerun commands](example-setup.md#require-both-directions-in-the-harness)
and [2026-11-01 results](../sdlc/changes/integration-read-write/validation.md).

See the [per-guide test results](../sdlc/changes/integration-guides-262/validation.md).

The [upstream acceptance audit](acceptance.md) covers all 51 tools in the
comparison, with Sling's standalone workflow recorded separately. The runner
wires reusable SQLAlchemy, DBI, Fivetran and ADBC
contracts into the testing harness, with explicit adapter gaps and skip counts.

Sling has a separate [PostgreSQL round-trip test](../../scripts/test_sling_postgres.py):
two 10,003-row round trips passed with zero value or duplicate differences on
verified Redis Software 4x QPF. It requires template overrides and direct loading
into a precreated Redis table; default staging and empty-result handling have
documented failures. These results are separate from the historical Python guide
sweep and upstream acceptance counts.

See [installation routes for all 51 inventory entries](../integration-installation.md).

Start with [installation and diagnostics](../setup.md) to choose a package and
verify the driver before configuring a tool.

Each guide covers Redis prerequisites, installation, a concrete integration path,
and its limits. These are separately maintained driver integrations; availability
of a guide does not imply upstream endorsement, release-wheel validation, or an
upstream contribution's acceptance.

## Browse by workflow

Guides use the same areas and categories as the
[ecosystem comparison](../data-engineering-adbc-support.md#grouped-inventory).
Only categories with dedicated guides are listed; DuckDB is included under
SQL & Arrow access.

| Area | Category |
|---|---|
| Data engineering | [DataFrames & arrays](#dataframes-arrays) |
| Data engineering | [Processing & streaming](#processing-streaming) |
| Data engineering | [SQL & Arrow access](#sql-arrow-access) |
| Data engineering | [Orchestration](#orchestration) |
| Data engineering | [ETL / ELT & integration](#etl-elt-integration) |
| Data science & BI | [ML, AI & scientific computing](#ml-ai-scientific-computing) |
| Data science & BI | [ML workflows & datasets](#ml-workflows-datasets) |
| Data science & BI | [Notebooks, IDEs & reporting](#notebooks-ides-reporting) |
| Data science & BI | [BI & visualization](#bi-visualization) |

## Data engineering

### DataFrames & arrays

| Integration | Dedicated guide |
|---|---|
| pandas | [SQL reads and DataFrame writes](pandas.md) |
| NumPy | [Arrow-to-array reads](numpy.md) |
| Polars | [URI/connection reads and DataFrame writes](polars.md) |

### Processing & streaming

| Integration | Dedicated guide |
|---|---|
| Apache Spark | [Setup, example and expected result](spark.md) |
| Apache Beam | [Setup, example and expected result](beam.md) |
| Dask | [Partitioned worker-local reads](dask.md) |
| Ray Data | [Worker-side Arrow reads and SQL APIs](ray.md) |

### SQL & Arrow access

| Integration | Dedicated guide |
|---|---|
| SQLAlchemy | [Dialect, reflection and autocommit](sqlalchemy.md) |
| PyArrow | [Setup, example and expected result](pyarrow.md) |
| R / DBI | [Setup, example and expected result](r-dbi.md) |
| Ibis | [Setup, example and expected result](ibis.md) |
| DuckDB | [Redis extension and adbc_scanner](../duckdb.md) |

### Orchestration

| Integration | Dedicated guide |
|---|---|
| Apache Airflow | [TaskFlow and the Redis hook](airflow.md) |
| Dagster | [ADBCResource and RedisADBCIOManager](dagster.md) |
| Prefect | [Task-local connections and flows](prefect.md) |
| Mage | [Setup, example and expected result](mage.md) |
| Kestra | [Setup, example and expected result](kestra.md) |

### ETL / ELT & integration

| Integration | Dedicated guide |
|---|---|
| dbt | [Core v1 starter project, seed/build/test and catalog](dbt.md) |
| Fivetran Connector SDK | [Setup, example and expected result](fivetran.md) |
| dlt | [Append-only custom destination](dlt.md) |
| [Sling](https://slingdata.io) | [PostgreSQL ↔ Redis ADBC round trips and configuration limits](sling.md) |

## Data science & BI

### ML, AI & scientific computing

| Integration | Dedicated guide |
|---|---|
| scikit-learn | [Feature loading and fit/predict](scikit-learn.md) |
| PyTorch | [Setup, example and expected result](pytorch.md) |
| TensorFlow | [Setup, example and expected result](tensorflow.md) |
| SciPy | [Sparse matrix construction and solving](scipy.md) |
| XGBoost | [Setup, example and expected result](xgboost.md) |
| Hugging Face Transformers | [Setup, example and expected result](transformers.md) |
| Keras | [Setup, example and expected result](keras.md) |

### ML workflows & datasets

| Integration | Dedicated guide |
|---|---|
| MLflow | [Setup, example and expected result](mlflow.md) |
| Kubeflow Pipelines | [Setup, example and expected result](kubeflow.md) |
| Metaflow | [Setup, example and expected result](metaflow.md) |
| Hugging Face Datasets | [Arrow conversion and local persistence](huggingface-datasets.md) |

### Notebooks, IDEs & reporting

| Integration | Dedicated guide |
|---|---|
| Jupyter | [Kernel installation and notebook queries](jupyter.md) |
| Quarto | [Setup, example and expected result](quarto.md) |
| RStudio | [Setup, example and expected result](rstudio.md) |

### BI & visualization

| Integration | Dedicated guide |
|---|---|
| Streamlit | [Bound queries and interactive app reruns](streamlit.md) |
| Plotly | [Setup, example and expected result](plotly.md) |
| Matplotlib | [Setup, example and expected result](matplotlib.md) |
| Apache Superset | [Driver installation, datasets and SQL Lab](superset.md) |

## Support and test evidence

[Support and test evidence](../data-engineering-adbc-support.md) distinguishes
recorded environments from unverified paths. Upstream contributions are tracked
in [issue #236](https://github.com/redis/redis-adbc-driver/issues/236) and the
existing per-integration issues linked there.

[Upstream contribution status](../upstream-submissions/status.md) records submitted
PRs and the prepared changes requiring human or maintainer action.

## Workflow provenance

[PR #248](https://github.com/redis/redis-adbc-driver/pull/248) adds NumPy,
scikit-learn, Jupyter notebook, Hugging Face Datasets, Streamlit and SciPy
workflows and has merged. Those guides document custom Python data paths and
their specific validation limits. Their examples use the public DB-API/Arrow
interface; use a checkout containing #248 for the saved workflow runner and tests.

See [new workflow documentation status](../upstream-submissions/status.md#new-workflows-in-pr-248)
for each upstream project, proposed documentation placement, and submission gaps.
[Issue #249](https://github.com/redis/redis-adbc-driver/issues/249) tracks the
guide contributions and remaining upstream submissions.

[Live example validation](../sdlc/changes/upstream-status-249/validation.md) records
Redis Software runs for all eighteen integration paths, including the six new
guides, the corrected DuckDB SQL and the dbt Core starter project.

## Maintaining the local harness

The standard-library [integration registry](../../python/scripts/integration_registry.py)
is the source for guide choices, guide read/write requirements, candidate pytest
selectors, profile membership, Python versions and dependency extras. Update it
when adding an integration; keep dependency pins in
[pyproject.toml](../../python/pyproject.toml). Guide names and candidate aliases
are distinct: for example, `huggingface-datasets` uses candidate `datasets`,
`pyarrow` uses `arrow`, and the R / DBI and RStudio guides share candidate `r`.
The guide and candidate execution orders are retained separately in the registry.
`STANDALONE_GUIDES` records integrations with their own external-tool runners;
Sling runs through `scripts/test_sling_postgres.py --sling /absolute/path/to/sling`
and is not a Python candidate profile or a Python dependency extra.

The runners keep explicit setup hooks for R/Quarto, Kestra and dbt. `ray-tls`
remains a separate execution mode that owns its TLS fixture. Registry queries
emit plain lines, never shell code. Run the offline contracts with:

```sh
python3 -m unittest discover -s scripts -p test_integration_registry.py -v
python3 -m unittest discover -s scripts -p test_candidate_tls_runner.py -v
```

Real Redis runs still require `make -C go redis-up` and verified effective
Software 4x QPF before tests; offline runner mocks do not establish integration
compatibility.
