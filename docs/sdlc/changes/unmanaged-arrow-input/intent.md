# Unmanaged Arrow input specialization

Requested November 13, 2026, after PR #431's broad experiment. Baseline is
`06c76c0b`. That older candidate remains unapplied: it helped unmanaged reads,
but did not consistently help managed reads. This is a new, separately frozen
hypothesis and full matched comparison, not adoption of the prior measurements.

Specialize existing wide explicit unmanaged Arrow output while leaving managed
bindings and their per-column hot loop unchanged. Preserve consistency, input
error order, NULL/type behavior, bounded ownership and all fallback paths.

See [contract](spec.md) and [plan](plan.md). No speedup or human approval is claimed.

Review: [PR #435](https://github.com/redis/redis-adbc-driver/pull/435) is open for maintainer review. No merge or human approval is claimed.
