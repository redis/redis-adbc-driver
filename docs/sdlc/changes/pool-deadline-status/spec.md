# Spec

2026-11-26; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Cause

Two worker pools run their workers under `context.WithCancelCause` of the statement's context and read the pool's cause after the workers return:

- **`parallel_topk.go`** (parallel aggregate top-k) returned `context.Cause(pool)` whenever it was set.
- **`join_snowflake.go`** (parallel streamed star/snowflake fact chunks) returned the cause unless it was `context.Canceled`, and otherwise `cancelled(ctx)`.

A worker that fails calls `cancel(err)`, so the cause is normally its error, which the driver has already wrapped. But when the statement's deadline passes first, the pool's cause is the parent's: the bare `context.DeadlineExceeded`. A statement `Cancel` gives `context.Canceled` in the same way. Both pools returned that error unwrapped. It reached the C boundary as a non-ADBC error, which `errToAdbcErr` reports as `UNKNOWN` with no SQLSTATE.

## Contract

`poolErr(ctx, pool)` in `cancel.go` (`ctx` is the statement's context, `pool` the one derived from it), used by both pools after their workers return:
- `nil` while the pool's context is not done.
- The pool's cause when a worker set it with its own error, including internal sentinels such as `errStarProbeStopped` that callers already handle.
- Otherwise `cancelled(ctx)`, or `cancelled(pool)` while the statement's context is not done. The statement's own state decides, as in the old snowflake code, so a passed deadline stays a timeout even when a worker cancelled the pool with a bare `context.Canceled`. That gives:
  - `TIMEOUT … the query timed out` with SQLSTATE 57014 after a deadline;
  - `CANCELLED … the query was cancelled` with SQLSTATE 57014 after a cancel;
  - the `adbc.Error` cause that `closeWith` set, unchanged.

The first cause wins, so a worker error recorded before the deadline is still reported as that error.

Unchanged:
- How workers check the context.
- The top-k merge and the snowflake capped/spill fallback (`errStarDimensionCap`).
- `partitioned_aggregate.go`, which already maps a bare context error through `cancelled`.

## Not covered

- A search for `context.Cause` and `return ctx.Err()` found no other such site on a query's execution path. The remaining bare `ctx.Err()` returns are in a connection-time probe (`flex.go`), publication and cleanup waits (`snapshot.go`, `snapshot_temp.go`) and live-table DDL (`live_evolve.go`); they were not changed. Errors built in other ways were not audited.
- The generated C bridge (`pkg/driver.go`) still maps any non-ADBC error to `UNKNOWN`; it has no context-error safety net.
