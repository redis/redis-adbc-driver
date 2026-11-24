# Plan and verification

Date: 2026-11-23. Public stack base: `c73e5b66bf45d8c0eb1203171c04b399dbcbc4ca` (#607). Local candidate.
See [intent](intent.md), [specification](spec.md).

1. Derive conservative typed fact-key demand after dimension preparation.
2. Add it to fresh fact source filters; retain original full join probes.
3. Verify caps, unsafe type fallback, NULLs, deterministic correlated supersets,
   exact large integers, cancellation, and actual native UNION propagation.
4. Parent independently reviews/composes with disjoint-batch dimension work and
   validates selected DS queries on backed-up Software with active verified 4x QPF.

Focused pure verification passed (0.998s), from `go/`:
`env -u REDIS_URI GOMAXPROCS=2 go test -count=1 -p 1 -run
'^(TestStarFactDemand|TestDeferredLeft|TestDeferredUnion|TestUnionStar|TestStar|TestDeferredDimension|TestDeferredComposite)' .`
Four new fact-demand tests plus prior focused regressions passed. The mocked
native composition verifies both UNION branches receive typed key predicates,
retain duplicate matches, and use complete bounded candidates with no full
row-directory enumeration. Formatting/diff checks passed. No native builds,
external SQL/Redis, containers, live artifacts, benchmarks, pushes, or issue
updates ran here. Independent review/native validation remain pending.

## Shared IN-list operand correction

Parent review identified that each generated equality used a fresh ColumnRef,
preventing literalInList's pointer-identity recognition. Reuse one column node
per independent key set. The new17-key regression evaluates the actual expression
and asserts one constructed/reused inLists hash set across matches, nonmatches,
NULL, and adjacent exact values beyond2^53. Focused combined tests passed1.026s:
`env -u REDIS_URI GOMAXPROCS=2 go test -count=1 -p 1 -run
'^(TestStarFactDemand|TestDeferredLeft|TestDeferredUnion|TestUnionStar|TestStar)' .`
No live execution or artifact changed; performance improvement needs native proof.

## Clean public stack preparation

This branch is based only on published #607 head `c73e5b66`, without the private
integration/LEFT ancestry. All unit-only Go packages passed on this final clean stack:
`env -u REDIS_URI GOMAXPROCS=2 go test -count=1 -p 1 ./...`
(root package 28.448s). Redis-URI integration skips are not native compatibility
evidence. The native composition mock is self-contained on published main
fixtures and does not depend on private adapter fields. Formatting/diff checks
passed.
No standalone native performance attribution is claimed. Publication remains
held; the parent owns any targeted Software validation.
