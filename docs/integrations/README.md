# Redis integration guides

All 38 entries in the README examples table link to a guide here or the DuckDB guide.
The [shared example setup](example-setup.md) supports the additional local workflows.
See the [per-guide test results](../sdlc/changes/integration-guides-262/validation.md).

Start with [installation and diagnostics](../setup.md) to choose a package and
verify the driver before configuring a tool.

Each guide covers Redis prerequisites, installation, a concrete integration path,
and its limits. These are separately maintained driver integrations; availability
of a guide does not imply upstream endorsement, release-wheel validation, or an
upstream contribution's acceptance.

| Integration | Dedicated guide |
|---|---|
| pandas | [SQL reads and DataFrame writes](pandas.md) |
| Polars | [URI/connection reads and DataFrame writes](polars.md) |
| SQLAlchemy | [Dialect, reflection and autocommit](sqlalchemy.md) |
| Apache Superset | [Driver installation, datasets and SQL Lab](superset.md) |
| Apache Airflow | [TaskFlow and the Redis hook](airflow.md) |
| Dagster | [ADBCResource and RedisADBCIOManager](dagster.md) |
| Prefect | [Task-local connections and flows](prefect.md) |
| dlt | [Append-only custom destination](dlt.md) |
| Ray Data | [Worker-side Arrow reads and SQL APIs](ray.md) |
| Dask | [Partitioned worker-local reads](dask.md) |
| DuckDB | [Redis extension and adbc_scanner](../duckdb.md) |
| dbt | [Core v1 starter project, seed/build/test and catalog](dbt.md) |
| NumPy | [Arrow-to-array reads](numpy.md) |
| scikit-learn | [Feature loading and fit/predict](scikit-learn.md) |
| Jupyter | [Kernel installation and notebook queries](jupyter.md) |
| Hugging Face Datasets | [Arrow conversion and local persistence](huggingface-datasets.md) |
| Streamlit | [Bound queries and interactive app reruns](streamlit.md) |
| SciPy | [Sparse matrix construction and solving](scipy.md) |
| PyArrow | [Setup, example and expected result](pyarrow.md) |
| PyTorch | [Setup, example and expected result](pytorch.md) |
| TensorFlow | [Setup, example and expected result](tensorflow.md) |
| Keras | [Setup, example and expected result](keras.md) |
| XGBoost | [Setup, example and expected result](xgboost.md) |
| Hugging Face Transformers | [Setup, example and expected result](transformers.md) |
| Plotly | [Setup, example and expected result](plotly.md) |
| Matplotlib | [Setup, example and expected result](matplotlib.md) |
| Ibis | [Setup, example and expected result](ibis.md) |
| MLflow | [Setup, example and expected result](mlflow.md) |
| Apache Spark | [Setup, example and expected result](spark.md) |
| Kubeflow Pipelines | [Setup, example and expected result](kubeflow.md) |
| Metaflow | [Setup, example and expected result](metaflow.md) |
| Fivetran Connector SDK | [Setup, example and expected result](fivetran.md) |
| Apache Beam | [Setup, example and expected result](beam.md) |
| Mage | [Setup, example and expected result](mage.md) |
| R / DBI | [Setup, example and expected result](r-dbi.md) |
| RStudio | [Setup, example and expected result](rstudio.md) |
| Quarto | [Setup, example and expected result](quarto.md) |
| Kestra | [Setup, example and expected result](kestra.md) |

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
