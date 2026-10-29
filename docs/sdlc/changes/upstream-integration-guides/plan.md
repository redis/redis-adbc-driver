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


## Explicit submission follow-up

The user explicitly requested submitting pandas, Ray, dlt and SQLAlchemy after
reviewable patches were published. Rebased the four branches onto current upstream
bases and submitted disclosed drafts: pandas #71772, Ray #67560, dlt #4621 and
SQLAlchemy #13741. No independent human review or maintainer approval was asserted.
Ray's commit carries DCO sign-off; its whitespace/EOF/size pre-commit checks and
repository-link checker passed, and the example compiles. SQLAlchemy's table and
references parse without docutils warnings; all four diffs pass whitespace checks.
No fresh live integrations or full upstream site builds were performed.

SQLAlchemy's gate closed #13741 because it referenced the closed original listing
issue #13706 without an approved open issue. Created documentation approval issue
#13742, linking the concrete patch and guide. Leave the PR closed until a maintainer
labels the approval issue `open for pull requests`; do not claim bypass or approval.
The other three drafts were checked open after submission. Preserved original
patches as artifacts and updated tracker #236 and existing per-integration issues.

Post-submission status links checked: 52 local links/anchors resolve; `git diff --check` passed.

## PR #237 merge-conflict repair

On 2026-10-29, merged main at `4053b2b` into the PR branch. Resolved the
introduction conflict in `docs/python-integrations.md` by retaining both the
dedicated-guide discovery link and main's Redis Search/QPF recommendation.
No integration runtime behavior changed.

Validation after reconciliation: 25 Python examples compile across the 17
Markdown files in the PR; 72 local links/anchors resolve; `git diff --check`
passes. Runtime integration suites and upstream site builds were not rerun.
