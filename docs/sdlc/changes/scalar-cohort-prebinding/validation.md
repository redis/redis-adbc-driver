# Validation

See [intent](intent.md), [specification](spec.md), [plan](plan.md).

Focused `TestScalarCohort` tests pass (0.708 seconds). The three added prebinding tests assert canonical metadata order, distinct predicate order, exact AVG input mapping, no runtime dependency on registration-only name maps, COUNT behavior, parity with generic decode/filter/type finalization, and atomic rejection before source execution. Existing parsed CASE/streaming/zero-outer/error/default/NULL/source-gate tests also passed.

All unit-only packages pass with `env -u REDIS_URI GOMAXPROCS=2 go test -p 1 -count=1 ./...` (root package 28.670 seconds). Applicable Redis-URI integration skips are not live compatibility evidence. Read-only independent review found no actionable issue in the binding proof, atomic publication or retained ordering/COUNT semantics; that reviewer did not run tests.

This branch is based on the frozen original prototype and does not bundle the separate inherited HASH-read guard correction. Any native integration must include public commit `91a79e93` (or its equivalent direct-Nil-only correction) as well. No native build, benchmark/SQL/container run, timing improvement or Q9 follow-up result is claimed by this change.

## Clean current-main publication

Only isolated 2c0b4fe0ff4b723d90b9df84a282b0456afba896 was extracted
onto public main f6e0bbbec1f9e1e6c973e7d7071b384ad174aaa8. Main includes
#610 and its direct-Nil-only HASH proof correction, so the dependency above is
satisfied on this publication branch. No private prototype ancestors are included.
Q9's earlier combined native6 result predates prebinding and is not validation
of this optimization. No isolated or native timing improvement is claimed.
Earlier no-push instructions above describe historical preparation; publication
is now authorized.

All endpoint-free unit packages pass on this clean publication branch: `env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 go test -count=1 -p 1 ./...` (root package 29.459s). Redis-dependent skips are not live compatibility evidence. Formatting/diff checks pass.
