# Upstream Redis integration documentation status

Checked October 30, 2026. Submission does not imply upstream approval or merge.
[Project tracker #236](https://github.com/redis/redis-adbc-driver/issues/236).
[Dedicated guide index](../integrations/README.md).
[Status-refresh issue #249](https://github.com/redis/redis-adbc-driver/issues/249).

| Integration | Dedicated guide | Upstream status |
|---|---|---|
| pandas | [Guide](../integrations/pandas.md) | [#71772](https://github.com/pandas-dev/pandas/pull/71772) is open and no longer a draft. Submitted with AI disclosure; upstream review and acceptance remain pending. Existing tracker #156. |
| Polars | [Guide](../integrations/polars.md) | New dedicated upstream guide: [#30441](https://github.com/pola-rs/polars/pull/30441). Existing project tracker #158. |
| Apache Airflow | [Guide](../integrations/airflow.md) | New dedicated upstream guide: [#75988](https://github.com/apache/airflow/pull/75988). |
| Apache Superset | [Guide](../integrations/superset.md) | New dedicated upstream guide: [#45852](https://github.com/apache/superset/pull/45852). Existing project tracker #159. |
| Prefect | [Guide](../integrations/prefect.md) | New dedicated upstream guide: [#23662](https://github.com/PrefectHQ/prefect/pull/23662), linked to required upstream issue #23661. |
| dlt | [Guide](../integrations/dlt.md) | [#4621](https://github.com/dlt-hub/dlt/pull/4621) is open and no longer a draft. Submitted with AI disclosure; upstream review and acceptance remain pending. Existing proposal #4587 is separate; approval is not asserted. Existing tracker #161. |
| Ray | [Guide](../integrations/ray.md) | [#67560](https://github.com/ray-project/ray/pull/67560) is open and no longer a draft, with AI disclosure and DCO sign-off. Relevant pre-commit and repository-link checks passed at submission; upstream review and acceptance remain pending. Existing tracker #164. |
| Dagster | [Guide](../integrations/dagster.md) | New dedicated upstream guide: [#371](https://github.com/dagster-io/community-integrations/pull/371), separate from generic IO-manager PR #362. Existing project tracker #157. |
| Dask | [Guide](../integrations/dask.md) | Existing [#12654](https://github.com/dask/dask/pull/12654) updated to link the dedicated guide and retained as a draft with the required AI disclosure. Existing project tracker #162. |
| SQLAlchemy | [Guide](../integrations/sqlalchemy.md) | Original external-dialect listing [#13707](https://github.com/sqlalchemy/sqlalchemy/pull/13707) is merged. Guide-link follow-up [#13741](https://github.com/sqlalchemy/sqlalchemy/pull/13741) and [approval issue #13742](https://github.com/sqlalchemy/sqlalchemy/issues/13742) are closed without the follow-up merging. [Maintainers prefer the canonical PyPI link](https://github.com/sqlalchemy/sqlalchemy/issues/13742#issuecomment-6523522556), where this project controls its documentation URL. Do not reopen the equivalent follow-up without new maintainer direction. |
| DuckDB | [Guide](../duckdb.md) | Existing full upstream Redis guide [#21](https://github.com/Query-farm/adbc_scanner/pull/21) remains open. Registry contribution #3157 separately merged. Existing project tracker #124. |
| dbt | [Guide](https://github.com/redis/redis-dbt-adapter/tree/main/v1) | Existing full upstream v1 setup/configuration guide [#10219](https://github.com/dbt-labs/docs.getdbt.com/pull/10219) remains open. Existing project tracker #171. |

See [policies and preserved patches](README.md) for exact review and approval
prerequisites. The four remaining contributions were submitted as drafts at the
user's explicit request after the concrete patches were provided. Submission is
not a claim that upstream prerequisites have been met. No duplicate per-integration project issues were created.
The umbrella issue aggregates existing trackers and new upstream PR links.

## Current outcome and verification

Eleven documentation PRs remain open: ten are ready for review and Dask remains
a draft. SQLAlchemy's original external-dialect listing is already merged; its
additional guide-link request was declined. All twelve integration paths have
an existing upstream documentation contribution. Do not create duplicate PRs.

The dated status above was verified against GitHub PR state, draft and merge
fields, and the SQLAlchemy maintainer responses. The SQLAlchemy follow-up was
[closed to match the issue response](https://github.com/sqlalchemy/sqlalchemy/pull/13741#issuecomment-6534589590).
Ready for review describes GitHub's non-draft state; it does not assert human
review, maintainer approval, passing checks or endorsement. Earlier test evidence
remains historical; no integration suites were rerun for this status refresh.
