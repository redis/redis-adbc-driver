# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

`probeSemiJoin` (`go/semijoin.go`) plans all of a page's batches first (each the
decorrelated body with a union of at most `semiProbeKeys` (256) first keys).
`runSemiProbes` runs them, and `probeSemiJoin` then records them in batch order.

## Workers

- With more than one batch and `parallelProbes` true, `runSemiProbes` takes
  `workerBudget.acquireBounded(ctx, e.queryWorkers, batches)` and releases it on
  return. The grant is bounded by the database budget, `adbc.redis.query_workers`
  and the batch count. With a grant of 1 (or no budget), batches run one after
  another as before.
- `parallelProbes` is false without a worker budget, under GPU predicates,
  data-modifying CTE state (`dmlSnapshots` / `dmlRunning`), retained or partition
  executors, a retained-allocation context, or a privately owned snapshot
  segment of the probed table. Those keep the serial path.
- Each worker runs on its own `isolatedChild()` with a fresh `newExecCache()` (the
  statement cache is not safe for concurrent writes) and a nil worker budget, so
  a probe nested in a body runs serially and never waits for tokens this probe
  holds. No other grant holder reaches `probeSemiJoin`: parallel aggregate,
  top-k, star-join and partitioned paths reject subquery predicates, and
  projection scans skip the probe hook.
- Workers take batches in order through an atomic index and share the
  statement's context. They never cancel a context of their own, because a
  cancelled command context interrupts every socket of the execution
  (`executionTransport.arm`) and would break the per-row fallback. After a
  failure, an atomic stop flag keeps new batches from starting; in-flight ones
  finish.
- A worker panic stops the others and is re-raised on the calling goroutine
  after all workers have stopped, after the grant is released by `defer`.

## Recording (unchanged semantics)

Batches are recorded in order up to the first that failed or did not run. That
batch sets `probeFailed`; earlier batches' complete groups are kept; later
outer keys run per row, and the per-row path reports the original error. An
unhashable key during recording also sets `probeFailed`. Cancellation records
nothing; the evaluation reports it.

`subqueryStats.parallelProbes` counts pages whose batches ran concurrently.

## Acceptance

- Serial (`query_worker_budget=1`) and parallel (`4`) databases return the same
  results for joined and scanned outer sides, with probes only (no per-row
  runs), and parallel pages only on the second.
- A body that fails (`10 / 0`) in one batch's group reports the same `division
  by zero` error in both modes. Without that order every probe succeeds.
- Semi-join and subquery suites pass, also under `-race`.
- Q4 / Q21 on the 10% subset validate and get faster than with
  `query_workers=1` on the same build and host.
