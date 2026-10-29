# Plan and verification: v0.0.18 benchmark evidence

Date: 2026-10-29. Status: prepared for review. See [intent](intent.md) and
[spec](spec.md).

## Scope and files

- Add the unchanged bulk runner, its four focused tests and usage notes.
- Add `benchmarks/v0018-refresh/2026-10-29/` with the report, repetition-level
  measurements, summary, audit and reproduction notes.
- Keep detailed operational evidence private. Do not commit deck exports or
  edit payloads. The Google Slides update was completed separately.

## Verification

Original measured runs completed October 29 using v0.0.18. Full Arrow fetch and
result checks passed. Worker-policy performance gates passed 10/12; the two 8×
misses remain visible. The full-scale section was explicitly skipped.

For this PR, the following checks pass:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 benchmarks/v0018-refresh/2026-10-29/audit.py
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s benchmarks/bulk-load -p 'test_*.py' -v
git diff --check
```

The helper tests ran with the pinned benchmark Python environment. The audit
recomputes IPC summaries and all twelve cold/warm policy gates from published
trials; it also checks visible medians/ranges and coverage. Relative Markdown
links and published artifact contents are inspected before submission.
No new production-driver change or additional live Redis test was introduced.

## Review

Self-review covers result interpretation, repetition arithmetic, destructive
fixture boundaries, links and publication privacy. No human approval or merge
is implied; PR review remains outstanding.
