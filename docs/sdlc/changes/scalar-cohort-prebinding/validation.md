# Validation

See [intent](intent.md), [specification](spec.md), [plan](plan.md).

Focused `TestScalarCohort` tests pass (0.708 seconds). The three added prebinding tests assert canonical metadata order, distinct predicate order, exact AVG input mapping, no runtime dependency on registration-only name maps, COUNT behavior, parity with generic decode/filter/type finalization, and atomic rejection before source execution. Existing parsed CASE/streaming/zero-outer/error/default/NULL/source-gate tests also passed.

All unit-only packages pass with `env -u REDIS_URI GOMAXPROCS=2 go test -p 1 -count=1 ./...` (root package 28.670 seconds). Applicable Redis-URI integration skips are not live compatibility evidence. Read-only independent review found no actionable issue in the binding proof, atomic publication or retained ordering/COUNT semantics; that reviewer did not run tests.

This branch is based on the frozen original prototype and does not bundle the separate inherited HASH-read guard correction. Any native integration must include public commit `91a79e93` (or its equivalent direct-Nil-only correction) as well. No native build, benchmark/SQL/container run, timing improvement or Q9 follow-up result is claimed by this change.
