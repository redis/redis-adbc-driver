# Implementation and evidence

Date: 2026-10-20. Source: `c74ebeb`. Status: local validation passed; CI pending.
See [intent](intent.md) and [spec](spec.md). Issue/PR: see the pull request containing these records.

1. Add a shared composite action for ccache installation, restoration and worker
   selection. Wire it into both DuckDB build jobs and print cache statistics.
2. Exclude Markdown-only PR changes from the existing path filters; cancel
   superseded release PR runs without changing release/dispatch behavior.
3. Validate workflow syntax, composite YAML and shell/Python snippets; exercise
   worker sizing on Linux/macOS fixtures and representative PR path matches.
4. Inspect the diff against REVIEW.md. Hosted cold/warm cache timing, memory use
   and complete matrix validation remain pending until the changes run in CI.

Local evidence (2026-10-20):

- `actionlint` 1.7.12 passed for all three modified workflows (exit 0).
- Ruby Psych parsed all five workflows and the composite action (exit 0).
- `bash -n` passed for both composite shell snippets; Python AST parsing passed
  for the embedded worker configuration script.
- Executed the actual embedded script with mocked CPU/memory discovery for six
  Linux/macOS fixtures: CPU cap, memory cap, four-worker cap and minimum-one
  behavior passed. Representative path checks preserved code/mixed PR matches,
  excluded Markdown-only matches and included composite action changes.
- Release publication/dispatch configuration and PR-only cancellation checks
  passed. `git diff --check` passed.
- Reviewed the diff using REVIEW.md: no driver behavior, permissions, test
  assertions or coverage matrices changed. No actionable finding remained in
  the local review; this is not human merge approval.

No hosted workflow has run with this change and no performance improvement is
claimed yet. Full DuckDB/Redis suites were not run locally for this CI-only
change. Hosted cache restore/save and memory/runtime measurements remain pending.
