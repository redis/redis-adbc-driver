# Specification

Date: 2026-11-23. Origin: follow-up to [PR606](https://github.com/redis/redis-adbc-driver/pull/606). Status: local candidate.
See [intent](intent.md), [plan](plan.md).

`deferredUnionRowBound` is a nonexecuting, overflow-checked upper bound through
already guarded single-consumer UNION ALL/pure projection plans. Sum published
physical managed rows (including invisible versions/tombstones as upper bounds)
or direct materialized-memory lengths. Refuse opaque/lazy/join/group, private,
unmanaged/live, unpinned, cleanup-pending, malformed, or overflowing inputs.
Qualified shallow view copies use the same physical plan and remain eligible.
No source reads, CTE materialization, or year inference occur during this proof.

`scanDeferredUnionPages` returns false before consuming anything for unsupported
requests, source trees, or retained-allocation execution contexts. Retained
execution keeps its established cumulative working-allocation admission path.
For supported ordinary contexts, preserve branch sequence,
duplicates, pairwise identity conversions, positional/qualified output names,
and full original source residual evaluation. Only existing total typed safe
constraints may move toward leaf sources. Use existing native scanPages for
leaf ownership, cursor cleanup, authoritative snapshot reads, and cancellation.
Project at most pipelineChunk rows into each ephemeral output page; consumers
must use their ordinary memory admission if they keep rows. This ephemeral
visitor does not run under retained-allocation contexts.

Preserve source-error priority of prior eager materialization: remember the first
consumer/residual error and stop callbacks, but finish source traversal so a
later authoritative source error takes precedence. Context cancellation aborts
promptly. Volatile or error-capable source projections retain original fallback.
No shared CTE is newly deferred and no aggregate result is recomputed per consumer.
The existing exact CASE, decimal SUM, NULL, and grouping accumulator machinery
consumes unchanged typed Values; this visitor does not implement arithmetic.

The standalone helper's integration boundary is a scanPages hook before ordinary
isMem materialization, with deferred facts admitted only after row-bound preflight.
The combined candidate now supplies that hook; see the [integration record](integration.md).
