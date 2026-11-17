# Upstream Redis integration documentation status

Checked **2026-11-17** through GitHub REST PR state, draft and merge fields.
[Umbrella #236](https://github.com/redis/redis-adbc-driver/issues/236),
[status tracker #249](https://github.com/redis/redis-adbc-driver/issues/249),
[guide index](../integrations/README.md),
[issue triage](../sdlc/changes/issue-triage-2026-11-17/triage.md).

The eighteen contributions below comprise **four merged, twelve open (all
non-draft), and two closed without merge**. These are documentation outcomes;
merge does not establish a new runtime compatibility result or endorsement.
Non-draft means GitHub permits review, without implying human review or approval.
No duplicate submissions or recurring monitoring were created.

| Integration | Project guide | Upstream contribution | Verified outcome | Tracker |
|---|---|---|---|---|
| pandas | [Guide](../integrations/pandas.md) | [#71772](https://github.com/pandas-dev/pandas/pull/71772) | Open; non-draft | #156 |
| Polars | [Guide](../integrations/polars.md) | [#30441](https://github.com/pola-rs/polars/pull/30441) | Open; non-draft | #158 |
| Apache Airflow | [Guide](../integrations/airflow.md) | [#75988](https://github.com/apache/airflow/pull/75988) | Open; non-draft | #272 |
| Apache Superset | [Guide](../integrations/superset.md) | [#45852](https://github.com/apache/superset/pull/45852) | Open; non-draft | #159 |
| Prefect | [Guide](../integrations/prefect.md) | [#23662](https://github.com/PrefectHQ/prefect/pull/23662) | Open; non-draft | #236 |
| dlt | [Guide](../integrations/dlt.md) | [#4621](https://github.com/dlt-hub/dlt/pull/4621) | Open; non-draft | #161 |
| Ray | [Guide](../integrations/ray.md) | [#67560](https://github.com/ray-project/ray/pull/67560) | Merged 2026-11-09 | #164 |
| Dagster | [Guide](../integrations/dagster.md) | [#371](https://github.com/dagster-io/community-integrations/pull/371) | Open; non-draft | #157 |
| Dask | [Guide](../integrations/dask.md) | [#12654](https://github.com/dask/dask/pull/12654) | Open; non-draft | #236 |
| SQLAlchemy | [Guide](../integrations/sqlalchemy.md) | [#13707](https://github.com/sqlalchemy/sqlalchemy/pull/13707) | Merged 2026-10-20 | #236 |
| DuckDB | [Guide](../duckdb.md) | [#21](https://github.com/Query-farm/adbc_scanner/pull/21) | Open; non-draft | #236 |
| dbt | [Guide](../integrations/dbt.md) | [#10219](https://github.com/dbt-labs/docs.getdbt.com/pull/10219) | Merged 2026-11-10 | #236 |
| NumPy | [Guide](../integrations/numpy.md) | [#33187](https://github.com/numpy/numpy/pull/33187) | Merged 2026-11-17 | #249 |
| scikit-learn | [Guide](../integrations/scikit-learn.md) | [#35280](https://github.com/scikit-learn/scikit-learn/pull/35280) | Closed without merge | #249 |
| Jupyter | [Guide](../integrations/jupyter.md) | [#838](https://github.com/jupyter/jupyter/pull/838) | Open; non-draft | #249 |
| Hugging Face Datasets | [Guide](../integrations/huggingface-datasets.md) | [#8911](https://github.com/huggingface/datasets/pull/8911) | Open; non-draft | #253 |
| Streamlit | [Guide](../integrations/streamlit.md) | [#1600](https://github.com/streamlit/docs/pull/1600) | Open; non-draft | #249 |
| SciPy | [Guide](../integrations/scipy.md) | [#26571](https://github.com/scipy/scipy/pull/26571) | Closed without merge | #249 |

## Maintainer decisions and compatibility boundaries

- Ray's [maintainer decision](https://github.com/ray-project/ray/pull/67560)
  selects a community connector link to this project's maintained guide instead
  of a dedicated Ray page. The final merged change adds one listing entry.
  Generic upstream ADBC support in Ray remains separate from this external adapter.
- SQLAlchemy's original listing is merged. The additional [guide-link PR #13741](https://github.com/sqlalchemy/sqlalchemy/pull/13741)
  closed without merge after [maintainers preferred the canonical PyPI link](https://github.com/sqlalchemy/sqlalchemy/issues/13742#issuecomment-6523522556).
  Do not reopen an equivalent request without new maintainer direction.
- scikit-learn [closed its submission for violating its automated-contributions policy](https://github.com/scikit-learn/scikit-learn/pull/35280).
  Independent human review was absent. This is a declined submission, with a
  project-local guide still available; do not treat it as pending review or resubmit
  an equivalent contribution without satisfying the upstream requirements.
- SciPy #26571 is closed without merge. No explanatory maintainer comment was
  returned by the issue-comments endpoint during this check; no reason is inferred.
- Superset 6.1 is currently unsupported by this project's Python 3.14+ package;
  see the [current guide](../integrations/superset.md) and merged
  [PR #472](https://github.com/redis/redis-adbc-driver/pull/472).
  An open historical documentation PR does not override that restriction.
- Airflow's generic-hook work remains in #272; Dask's worker-local Arrow sink
  remains in #273. Open guide contributions do not resolve those feature gaps.

## Historical validation and submission provenance

The previous October 30 snapshot is retained in git history. Its eleven-open-PR
count, Dask draft status and six-new-drafts wording are historical. Driver-side
[PR #248](https://github.com/redis/redis-adbc-driver/pull/248) merged on October 30;
its workflows are no longer an unmerged proposal.

The [live guide validation](../sdlc/changes/upstream-status-249/validation.md)
records the earlier eighteen-path Software run with verified 4x QPF. Original
[submission policies and patches](README.md) and [six-workflow records](new-workflows/README.md)
retain their source revisions, AI disclosures, review requirements and validation
limits. The explicit-shard empty-Dataset workaround remains required by the
recorded integration contract (#253). Earlier Redis OSS evidence stays historical.
This status refresh runs no new integration suite and makes no new performance,
hosted-site, multi-node or release-wheel compatibility claim.
