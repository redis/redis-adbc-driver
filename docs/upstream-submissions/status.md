# Upstream Redis integration documentation status

Checked October 29, 2026. Submission does not imply upstream approval or merge.
[Project tracker #236](https://github.com/redis/redis-adbc-driver/issues/236).
[Dedicated guide index](../integrations/README.md).

| Integration | Dedicated guide | Upstream status |
|---|---|---|
| pandas | [Guide](../integrations/pandas.md) | [Patch](pandas.patch) prepared; human must submit under pandas policy. Existing project tracker #156. |
| Polars | [Guide](../integrations/polars.md) | New dedicated upstream guide: [#30441](https://github.com/pola-rs/polars/pull/30441). Existing project tracker #158. |
| Apache Airflow | [Guide](../integrations/airflow.md) | New dedicated upstream guide: [#75988](https://github.com/apache/airflow/pull/75988). |
| Apache Superset | [Guide](../integrations/superset.md) | New dedicated upstream guide: [#45852](https://github.com/apache/superset/pull/45852). Existing project tracker #159. |
| Prefect | [Guide](../integrations/prefect.md) | New dedicated upstream guide: [#23662](https://github.com/PrefectHQ/prefect/pull/23662), linked to required upstream issue #23661. |
| dlt | [Guide](../integrations/dlt.md) | [Community listing patch](dlt.patch) prepared; upstream #4587 awaits maintainer decision and human-reviewed submission. Existing project tracker #161. |
| Ray | [Guide](../integrations/ray.md) | [Dedicated upstream guide patch](ray.patch) prepared; human review/local verification required before submitting under Ray policy. Existing project tracker #164. |
| Dagster | [Guide](../integrations/dagster.md) | New dedicated upstream guide: [#371](https://github.com/dagster-io/community-integrations/pull/371), separate from generic IO-manager PR #362. Existing project tracker #157. |
| Dask | [Guide](../integrations/dask.md) | Existing [#12654](https://github.com/dask/dask/pull/12654) updated to link the dedicated guide and retained as a draft with the required AI disclosure. Existing project tracker #162. |
| SQLAlchemy | [Guide](../integrations/sqlalchemy.md) | Listing [#13707](https://github.com/sqlalchemy/sqlalchemy/pull/13707) merged; [guide-link follow-up patch](sqlalchemy.patch) prepared pending approved issue and human-written submission. |
| DuckDB | [Guide](../duckdb.md) | Existing full upstream Redis guide [#21](https://github.com/Query-farm/adbc_scanner/pull/21) remains open. Registry contribution #3157 separately merged. Existing project tracker #124. |
| dbt | [Guide](https://github.com/redis/redis-dbt-adapter/tree/main/v1) | Existing full upstream v1 setup/configuration guide [#10219](https://github.com/dbt-labs/docs.getdbt.com/pull/10219) remains open. Existing project tracker #171. |

See [handoff policies and patches](README.md) for exact prerequisites that prevent
agent submission. No duplicate per-integration project issues were created.
The umbrella issue aggregates existing trackers and new upstream PR links.
