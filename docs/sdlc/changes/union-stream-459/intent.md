# Intent: incremental UNION ALL (#459)

Date: 2026-10-22. Requestor: Albert Wong. Author: Codex.
Base: `d4cbccf`. Status: implemented and verified; awaiting review.

Implement finite incremental output for UNION ALL over eligible indexed table
scans, preserving pairwise common-type coercion, duplicates, parameters and
pagination. Keep one active child source, release it on completion/cancellation,
and bound pending result state by scan pages and output batch settings.

Do not add CDC (#457), DISTINCT set algorithms, joins/windows, spill or a universal
memory bound. Assess these candidates explicitly and retain their existing
paths. Deliver correctness/lifecycle tests, independent full-value validation
and measurements across input sizes and duplicate/group cardinalities.

Links: [#459](https://github.com/alberttwong/redis-adbc-driver/issues/459),
[spec](spec.md), [plan](plan.md), [evidence](evidence.md).
