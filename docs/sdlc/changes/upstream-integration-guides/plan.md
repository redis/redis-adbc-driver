# Plan and verification

Date: 2026-10-29. Author/self-reviewer: Codex. Base: `46d4387`.
See [intent](intent.md) and [spec](spec.md).

Created ten guides from the existing integration contracts and linked twelve
integration paths from a single index. Added discovery from the Python guide.
Review focused on copied example completeness, relative links, package extras,
source-only helper availability and non-transactional writes.

Validation: 13 Python examples compile; 32 local links/anchors resolve;
`git diff --check` passed. Existing runtime evidence is linked, not rerun.
Human approval and upstream acceptance are not implied.


## Upstream submission results

Five dedicated guides submitted: Dagster #371, Polars #30441, Airflow #75988,
Superset #45852, Prefect #23662 (its required upstream issue is #23661).
Dask #12654 updated with dedicated guide links; its open-pr skill requires draft
state, title prefix and AI disclaimer, all preserved.

Polars dprint checks passed. Superset changed-file pre-commit checks passed.
Prefect Mintlify links/anchors and changed-file pre-commit checks passed.
Dagster, Polars, Prefect and Airflow Python examples compile; JSON examples parse.
Airflow's new Sphinx reference and toctree entries resolve locally. Full site
builds and live integrations were not rerun for these docs contributions.

Four reviewable patches are retained under docs/upstream-submissions for pandas,
Ray, dlt and SQLAlchemy. Their exact human-review/maintainer-approval requirements
and existing issue links are recorded there. Those patches were not submitted.
Existing DuckDB and dbt PRs already contain dedicated guides; no duplicate PRs
were opened. Reused project trackers and created umbrella issue #236 at the user's
explicit request. No upstream review outcome or human approval is implied.
