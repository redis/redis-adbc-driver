# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Microbenchmark serial and concurrent 256-key probes on the 10% subset.
2. Confirm that no existing worker-budget grant holder can reach `probeSemiJoin`
   (subquery predicates are rejected by every parallel path).
3. Split `probeSemiJoin` into planning, `runSemiProbes` and ordered recording;
   gate concurrency with `parallelProbes`; use the worker budget.
4. Add `TestSQLSemiJoinParallelProbes` (serial vs parallel databases, failing
   probe body) and run it, the semi-join and subquery suites with `-race`, the
   wider subquery selection and the unit-only pass.
5. Measure Q4 / Q21 on the subset against `query_workers=1` on the same build,
   and the probe-versus-whole-build crossover.
6. SF1 Q21 / Q4 on the issue profile is verified separately before closing #587
   or #580.

Possible follow-up (not in this change): a cost-based switch to the whole
decorrelated build for in-memory outer sides, whose distinct keys are all known
before probing. The measured crossover (see validation) is far from Q4 / Q21.

No merge, release or human review approval is inferred.
