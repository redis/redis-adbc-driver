# Specification: incremental UNION ALL

Date: 2026-10-22. Author: Codex. Base: `d4cbccf`. Status: implemented and verified; awaiting review.
See [intent](intent.md), [plan](plan.md), and [#197](https://github.com/redis/redis-adbc-driver/issues/197).

Enable only trees of UNION ALL without ORDER BY, with eligible managed indexed
row-local leaf scans, or statically empty leaves. Preflight all scan choices
without opening cursors; any incompatible leaf falls back to the complete
materialized executor. Retain materialization for value-dependent decimal
schemas, key lookups, views/CTEs, subqueries, joins, windows and distinct set operations.

Execute children sequentially and retain the original pairwise set-operation
tree: coerce each row at each pairwise boundary in the session time zone.
Apply each node's OFFSET and LIMIT after coercion. LIMIT can stop before opening
later children; errors in children that are reached surface when those batches
are fetched. Planning/type errors remain eager. Names come from the leftmost
branch. Preserve duplicates and child traversal order without claiming an SQL
ordering guarantee in the absence of ORDER BY.

Reuse the stream owner registry, cancellation context, metadata-read checks,
row/byte batch limits and Arrow ownership. First fill errors return from
ExecuteQuery; later errors end the stream. No child opens before needed; close
an active child on completion, error, release, cancellation or connection close.
No detached Arrow buffer becomes invalid when a cursor is cancelled.

The generic Value path bounds pending rows to the active child's cursor-page
overshoot plus output batches; total branch cardinality is not retained.
One wide row, scan page, schema/plan state and consumer-held Arrow batches remain
outside any process RSS guarantee. There is no new parallel worker admission.
