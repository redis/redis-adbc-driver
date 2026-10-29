# Upstream documentation submissions and preserved patches

These patches were prepared for review, then submitted as disclosed draft PRs at
the user's explicit request. See [current submission status](status.md). The
patches are retained as the original reviewable artifacts; submission does not
imply that human-review or maintainer-approval requirements have been met. Dedicated guides are in
[the integration index](../integrations/README.md). Tracking issue: [#236](https://github.com/redis/redis-adbc-driver/issues/236).

| Integration | Patch | Remaining prerequisite |
|---|---|---|
| pandas | [Ecosystem guide link](pandas.patch) | Draft [#71772](https://github.com/pandas-dev/pandas/pull/71772) submitted; human review remains pending under pandas policy. |
| Ray | [Dedicated guide and loading-page link](ray.patch) | Draft [#67560](https://github.com/ray-project/ray/pull/67560) submitted; human review remains pending under Ray policy. |
| dlt | [Community destination guide link](dlt.patch) | Draft [#4621](https://github.com/dlt-hub/dlt/pull/4621) submitted; maintainer decision on #4587 and human review remain pending. |
| SQLAlchemy | [Link from the merged dialect listing to the dedicated guide](sqlalchemy.patch) | Draft [#13741](https://github.com/sqlalchemy/sqlalchemy/pull/13741) automatically closed; [approval issue #13742](https://github.com/sqlalchemy/sqlalchemy/issues/13742) requests the required maintainer label. Original listing #13707 is merged. |

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
The submissions were rebased onto current upstream branches before opening the
drafts. Review against newer upstream changes before further updates. Patch guide links
pin driver revision `f78bcb7`, so they resolve before the guide-index PR merges.
Patch validation: `git diff --check`; Ray's Python example compiles and its
signature matches the implemented reader. No new Redis integration tests or
full upstream builds were run to prepare these documentation handoffs.

Dask's existing PR was updated separately under its
[open-pr skill](https://github.com/dask/dask/blob/main/.agents/skills/open-pr/SKILL.md):
"The PR MUST be set to draft state" with its mandatory AI disclaimer. Draft
submission is not human approval.
