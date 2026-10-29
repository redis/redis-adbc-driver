# Prepared upstream documentation handoffs

These reviewable patches are not submitted upstream. Dedicated guides are in
[the integration index](../integrations/README.md). Tracking issue: [#236](https://github.com/redis/redis-adbc-driver/issues/236).

| Integration | Patch | Remaining prerequisite |
|---|---|---|
| pandas | [Ecosystem guide link](pandas.patch) | Human submission: pandas AGENTS.md prohibits agents from issue/PR actions on the user's behalf. |
| Ray | [Dedicated guide and loading-page link](ray.patch) | Human review of every changed line and relevant local checks before a PR, as required by Ray's policy. |
| dlt | [Community destination guide link](dlt.patch) | Maintainer decision on existing proposal #4587 and human-reviewed submission. |
| SQLAlchemy | [Link from the merged dialect listing to the dedicated guide](sqlalchemy.patch) | A maintainer-approved issue labeled `open for pull requests`; the earlier listing PR #13707 is already merged. |

## Policies checked

- [pandas AGENTS.md](https://github.com/pandas-dev/pandas/blob/main/AGENTS.md):
  "Never perform issue- or pull-request-related actions ... on behalf of the user."
- [Ray AGENTS.md](https://github.com/ray-project/ray/blob/master/AGENTS.md):
  "Pure code-agent PRs are not allowed" and the submitting human must review
  every changed line and run the relevant tests locally.
- [dlt contribution policy](https://github.com/dlt-hub/dlt/blob/devel/CONTRIBUTING.md):
  autonomous submissions are not accepted; non-trivial documentation work needs
  maintainer approval. Existing issue [#4587](https://github.com/dlt-hub/dlt/issues/4587)
  had `needs decision` and no approval when checked on October 29, 2026.
- [SQLAlchemy contribution policy](https://github.com/sqlalchemy/sqlalchemy/blob/main/.github/CONTRIBUTING.md):
  "This applies to documentation and typo fixes too" under the requirement for
  an issue labeled `open for pull requests`. The previous listing issue #13706
  is closed and has no current approval label.

Apply each patch to its respective repository, not this driver repository.
Review against the latest upstream branch before submission. Patch guide links
pin driver revision `f78bcb7`, so they resolve before the guide-index PR merges.
Patch validation: `git diff --check`; Ray's Python example compiles and its
signature matches the implemented reader. No new Redis integration tests or
full upstream builds were run to prepare these documentation handoffs.

Dask's existing PR was updated separately under its
[open-pr skill](https://github.com/dask/dask/blob/main/.agents/skills/open-pr/SKILL.md):
"The PR MUST be set to draft state" with its mandatory AI disclaimer. Draft
submission is not human approval.
