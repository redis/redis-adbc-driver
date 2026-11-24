# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

`scanIter.keep` finds the decorrelated EXISTS / scalar IN subqueries directly in
its residual (`semiProbeSubqueries`; `walkExpr` does not enter subquery bodies).
When there are any, it decodes the page once, calls `probeSemiJoin` for each,
then filters with the same decoded rows.

`probeSemiJoin` applies only while the semi-join is unbuilt, not eager, not
failed and not marked `probeFailed`. The decorrelated body's first key must be an
indexed column with a simple field name (`lookupColumn`). For the page's outer
rows whose full key tuple is neither NULL nor already probed, it collects the
distinct first-key values. Integer values outside exact double range stop
probing. It then runs the decorrelated body once per batch of at most
`semiProbeKeys` (256) values, adding `first_key = v1 OR …`. That is the same
union the planner already pushes for IN lists, and `unionQuery` must accept the
batch. Only key tuples of this page whose first key is in the batch are recorded
in `probed`, and only rows with those keys are added to `groups`, so every
recorded group is complete. Rows the union returns for other keys are ignored.

`evalSemiJoin` answers from `groups` when the semi-join is built, or when the
outer key is NULL or probed. Otherwise the per-row path runs as before. Any
probe failure (a body error, an unhashable key, a non-union key) sets
`probeFailed`, so the original per-row runs and adaptive whole build report the
original query's results and errors. Probing replaces per-row runs, so the
adaptive whole build happens only after a probe failure.

The 256-key batch is a measured bound: Search re-evaluates a key union for
each result page, so per-key cost rose from 0.41 ms at 250 keys to 0.60 ms at
1,000 keys (four lineitems per order).

Acceptance: Q4 on the subset completes and validates; semi-join result tests
pass unchanged. Probe work counts replace the old per-row/build expectations.
A new case covers more than one batch, NULL keys, missing keys, NOT EXISTS
and correlated IN. SF1 Q4 completes under 60 s on the issue profile.
