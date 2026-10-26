# Specification

2026-10-22; source `d4cbccf`; requestor: Albert Wong; author: Codex.

See [intent](intent.md) and [plan](plan.md).

All three issue forms apply an existing repository label at creation. Blank submissions are disabled for contributors. Agents must include labels in CLI/API creation requests.

GitHub does not provide a universal submission gate: maintainers retain blank submissions and API clients can bypass forms. A workflow repairs unlabeled issues after opening, reopening, or removal of the last label, using `needs-triage`. It preserves existing labels and has only `issues: write` permission, with no checkout or execution of issue content. No historical backfill is included.

The forms and workflow become active on the default branch. Refer to [GitHub template configuration](https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/configuring-issue-templates-for-your-repository) and [form syntax](https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/syntax-for-issue-forms). Labels must continue to exist in repository settings.
