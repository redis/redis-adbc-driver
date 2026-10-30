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
| dbt | [Guide](../integrations/dbt.md) | Existing full upstream v1 setup/configuration guide [#10219](https://github.com/dbt-labs/docs.getdbt.com/pull/10219) remains open. Existing project tracker #171. |

See [policies and preserved patches](README.md) for exact review and approval
prerequisites. The four remaining contributions were submitted as drafts at the
user's explicit request after the concrete patches were provided. Submission is
not a claim that upstream prerequisites have been met. No duplicate per-integration project issues were created.
The umbrella issue aggregates existing trackers and new upstream PR links.

## Current outcome and verification

Eleven documentation PRs remain open: ten are ready for review and Dask remains
a draft. SQLAlchemy's original external-dialect listing is already merged; its
additional guide-link request was declined. All twelve established integration paths have
an existing upstream documentation contribution. Do not create duplicate PRs.

The dated status above was verified against GitHub PR state, draft and merge
fields, and the SQLAlchemy maintainer responses. The SQLAlchemy follow-up was
[closed to match the issue response](https://github.com/sqlalchemy/sqlalchemy/pull/13741#issuecomment-6534589590).
Ready for review describes GitHub's non-draft state; it does not assert human
review, maintainer approval, passing checks or endorsement. Earlier test evidence
remains historical. A subsequent user-requested live guide run is recorded in
[validation](../sdlc/changes/upstream-status-249/validation.md).

## New workflows in PR #248

The twelve integrations above are the established guide inventory, not the
complete set of recent workflow additions. [PR #248](https://github.com/redis/redis-adbc-driver/pull/248)
adds six additional custom Python paths and is still open and unmerged as of
October 30, 2026. Dedicated guides are provided below; their upstream proposals remain outstanding.
They are not covered by the eleven existing open documentation PRs.

| New integration | Dedicated guide | Recorded scope in #248 | Upstream documentation target reviewed | Submission status |
|---|---|---|---|---|
| NumPy | [Guide](../integrations/numpy.md) | Arrow query column converted to a numeric NumPy array. | [NumPy user guide](https://github.com/numpy/numpy/tree/main/doc/source/user); propose an Arrow/database interoperability example. | Not submitted; document source-build workflow and seek suitable placement. |
| scikit-learn | [Guide](../integrations/scikit-learn.md) | Arrow-to-NumPy features/labels, then small decision-tree fit/predict. | [Data interoperability](https://github.com/scikit-learn/scikit-learn/blob/main/doc/data_interoperability.rst); propose a data-loading example. | Not submitted; custom pipeline, not a native estimator or connector. |
| Jupyter | [Guide](../integrations/jupyter.md) | Separate Python kernel executes queries and retains typed results and saved notebook output. | [Jupyter data-science use cases](https://github.com/jupyter/jupyter/tree/master/docs/source/use/use-cases); propose a runnable Redis notebook example. | Not submitted; JupyterLab browser UI is outside the recorded validation. |
| Hugging Face Datasets | [Guide](../integrations/huggingface-datasets.md) | Arrow-to-Dataset conversion, local save/reload/filter, including empty results. | [PyArrow interoperability](https://github.com/huggingface/datasets/blob/main/docs/source/use_with_pyarrow.mdx); propose an external SQL/Arrow example. | Not submitted; no Hub/model or remote dataset validation. |
| Streamlit | [Guide](../integrations/streamlit.md) | AppTest displays queried rows and reruns a bound-filter query when an input changes. | [Database tutorials](https://github.com/streamlit/docs/tree/main/content/develop/tutorials/databases); propose a Redis ADBC tutorial. | Not submitted; browser and hosted deployment are outside the recorded validation. |
| SciPy | [Guide](../integrations/scipy.md) | Stored sparse coefficients become CSR data for a linear solve with solution/residual checks. | [Sparse tutorial](https://github.com/scipy/scipy/blob/main/doc/source/tutorial/sparse.rst); propose an external-data example. | Not submitted; custom application data path, not a SciPy database connector. |

The [source-specific validation record](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/docs/sdlc/changes/candidate-test-runner/validation.md)
reports five passing Jupyter/Datasets/Streamlit/SciPy workflow tests on Redis
Software 8.6.2 with Search and verified active 4x QPF. NumPy and scikit-learn's
two passing tests are separately recorded in the
[plan](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/docs/sdlc/changes/candidate-test-runner/plan.md)
against Redis OSS; they were not rerun on Software by this documentation task.
The Dataset workflow explicitly uses `num_shards=1` for empty save/reload.

Before upstream submission, check each project's contribution requirements and
existing proposals, link the dedicated guide, and state
the source revision and exact validated scope. Until #248 merges, contributions
must disclose that these workflows and their evidence live in an unmerged PR.
[Issue #249](https://github.com/redis/redis-adbc-driver/issues/249) tracks this
additional scope as well as the earlier contributions. No new upstream PRs for
these six workflows have been submitted by this status refresh.

The [live guide validation](../sdlc/changes/upstream-status-249/validation.md)
also executes all eighteen integration paths against isolated Redis Software
with Search and active verified 4x QPF. The six new examples passed, including
notebook-kernel execution and the Streamlit input rerun. Upstream submissions
remain a separate outstanding step.
