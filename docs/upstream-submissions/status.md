# Upstream Redis integration documentation status

Checked October 29, 2026. Submission does not imply upstream approval or merge.
[Project tracker #236](https://github.com/redis/redis-adbc-driver/issues/236).
[Dedicated guide index](../integrations/README.md).

| Integration | Dedicated guide | Upstream status |
|---|---|---|
| pandas | [Guide](../integrations/pandas.md) | Submitted [#71772](https://github.com/pandas-dev/pandas/pull/71772) as a disclosed draft at the user's explicit request. Human review and acceptance remain pending. Existing tracker #156. |
| Polars | [Guide](../integrations/polars.md) | New dedicated upstream guide: [#30441](https://github.com/pola-rs/polars/pull/30441). Existing project tracker #158. |
| Apache Airflow | [Guide](../integrations/airflow.md) | New dedicated upstream guide: [#75988](https://github.com/apache/airflow/pull/75988). |
| Apache Superset | [Guide](../integrations/superset.md) | New dedicated upstream guide: [#45852](https://github.com/apache/superset/pull/45852). Existing project tracker #159. |
| Prefect | [Guide](../integrations/prefect.md) | New dedicated upstream guide: [#23662](https://github.com/PrefectHQ/prefect/pull/23662), linked to required upstream issue #23661. |
| dlt | [Guide](../integrations/dlt.md) | Submitted [#4621](https://github.com/dlt-hub/dlt/pull/4621) as a disclosed draft. Existing upstream #4587 still needs a maintainer decision; approval is not asserted. Existing tracker #161. |
| Ray | [Guide](../integrations/ray.md) | Submitted [#67560](https://github.com/ray-project/ray/pull/67560) as a disclosed draft with DCO sign-off. Relevant pre-commit and repository-link checks passed; human review remains pending. Existing tracker #164. |
| Dagster | [Guide](../integrations/dagster.md) | New dedicated upstream guide: [#371](https://github.com/dagster-io/community-integrations/pull/371), separate from generic IO-manager PR #362. Existing project tracker #157. |
| Dask | [Guide](../integrations/dask.md) | Existing [#12654](https://github.com/dask/dask/pull/12654) updated to link the dedicated guide and retained as a draft with the required AI disclosure. Existing project tracker #162. |
| SQLAlchemy | [Guide](../integrations/sqlalchemy.md) | Submitted [#13741](https://github.com/sqlalchemy/sqlalchemy/pull/13741) as draft; automatically closed for referencing a closed issue without approval. New [approval issue #13742](https://github.com/sqlalchemy/sqlalchemy/issues/13742) requests the required maintainer decision before reopening. Original listing #13707 remains merged. |
| DuckDB | [Guide](../duckdb.md) | Existing full upstream Redis guide [#21](https://github.com/Query-farm/adbc_scanner/pull/21) remains open. Registry contribution #3157 separately merged. Existing project tracker #124. |
| dbt | [Guide](https://github.com/redis/redis-dbt-adapter/tree/main/v1) | Existing full upstream v1 setup/configuration guide [#10219](https://github.com/dbt-labs/docs.getdbt.com/pull/10219) remains open. Existing project tracker #171. |

See [policies and preserved patches](README.md) for exact review and approval
prerequisites. The four remaining contributions were submitted as drafts at the
user's explicit request after the concrete patches were provided. Submission is
not a claim that upstream prerequisites have been met. No duplicate per-integration project issues were created.
The umbrella issue aggregates existing trackers and new upstream PR links.
