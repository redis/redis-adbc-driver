# Redis integration guides

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
| dbt | [Redis adapter setup and project guide](https://github.com/redis/redis-dbt-adapter/tree/main/v1) |
| NumPy | [Arrow-to-array reads](numpy.md) |
| scikit-learn | [Feature loading and fit/predict](scikit-learn.md) |
| Jupyter | [Kernel installation and notebook queries](jupyter.md) |
| Hugging Face Datasets | [Arrow conversion and local persistence](huggingface-datasets.md) |
| Streamlit | [Bound queries and interactive app reruns](streamlit.md) |
| SciPy | [Sparse matrix construction and solving](scipy.md) |

[Support and test evidence](../data-engineering-adbc-support.md) distinguishes
recorded environments from unverified paths. Upstream contributions are tracked
in [issue #236](https://github.com/redis/redis-adbc-driver/issues/236) and the
existing per-integration issues linked there.

[Upstream contribution status](../upstream-submissions/status.md) records submitted
PRs and the prepared changes requiring human or maintainer action.

## Additional workflows under review

[PR #248](https://github.com/redis/redis-adbc-driver/pull/248) adds NumPy,
scikit-learn, Jupyter notebook, Hugging Face Datasets, Streamlit and SciPy
workflows. That PR is still unmerged. The six additional guides above document custom Python
data paths and their specific validation limits. Their examples use the public
DB-API/Arrow interface; the saved workflow runner and tests require #248.

See [new workflow documentation status](../upstream-submissions/status.md#new-workflows-in-pr-248)
for each upstream project, proposed documentation placement, and submission gaps.
[Issue #249](https://github.com/redis/redis-adbc-driver/issues/249) tracks the
guide contributions and remaining upstream submissions.
