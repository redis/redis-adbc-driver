# Specification

2026-11-25; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

`semiJoinState` records the inner side's planning count (`countSnapshotCandidates` or
`countMatches`, an upper bound) as `semiResult.inner`.

`scanMem` calls `wholeBuildByCost` once per decorrelated semi-join in its residual, with the
complete in-memory outer rows, before the first page probe. It does nothing when:
- the state is already built, failed, eager, probing has failed, or a probe already ran;
- the inner count is unknown or zero;
- the inner side is in memory;
- the test seam `testSkipSemiCostBuild` is set.

Otherwise it counts the outer rows' distinct non-NULL first-key values, the key the probe
batches use. It checks cancellation every `semiProbePage` rows. It builds the inner side once
as soon as:

    distinct × semiProbeKeyCost / min(workers, ⌈distinct / 256⌉) > inner × semiWholeBuildRowCost

- `semiWholeBuildRowCost` is 10 µs and `semiProbeKeyCost` is 350 µs (see validation).
- `workers` is what the probes could be granted: the worker budget's capacity, capped by the
  connection's query workers when `parallelProbes` allows parallel batches, else 1.
- A key that can't be hashed, or a first-key column absent from the outer types, stops the check.
  Probing then continues as before.

The build uses `buildSemiJoin` into a fresh state. If it succeeds, its groups, distinct values
and row count move into the statement's state. The probes then return early and every outer
row reads the built groups, as an eager build would. If it fails, the state is unchanged: page
probes, their per-row fallback and the original errors behave exactly as before.
`subqueryStats.costBuilds` counts the builds.

Unchanged: scanned outer sides (`scanIter.keep`), the probe batch size, eager and Flex
whole builds, NULL and three-valued IN / NOT IN semantics, residual evaluation and duplicates.

Acceptance:
- `TestSQLSemiJoinWholeBuildByCost` checks, with one worker, that an outer side below the
  threshold keeps page probes and one above it builds once, with the same results.
- The above-threshold case fails when the switch is off (main's behavior).
- The page-probe tests keep exercising probes (`keepSemiProbes`), and the semi-join and
  subquery selections pass under `-race`.
- On the subset, a join with 73,252 distinct outer keys and one worker takes the whole build.
  TPC-H Q4 / Q21 stay on probes and validate.
