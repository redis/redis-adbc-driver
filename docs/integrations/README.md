# Redis integration guides

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

[Support and test evidence](../data-engineering-adbc-support.md) distinguishes
recorded environments from unverified paths. Upstream contributions are tracked
in [issue #236](https://github.com/redis/redis-adbc-driver/issues/236) and the
existing per-integration issues linked there.
