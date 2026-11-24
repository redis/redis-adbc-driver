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

## Current-main publication and combined evidence

Reapplied only clean public b70fd563 (08a26a3c plus b2cd6495) onto
main f6e0bbbec1f9e1e6c973e7d7071b384ad174aaa8, after #607 merged.
No private integration ancestors are included.

Original SF1 Q5 validated 100 rows/5 columns in combined source
119633325dc33d8fd633c3bfea71375ba43b245b (v0.0.31-dsfix6),
library SHA256 200168f2dd888982b33353f23ae7faf46cff5bf4c5f784f84a0db13f15fd6249.
Warm-up 8.051566086s; timed 3.463930335/2.881045418/3.183502876s;
median 3.183502876s, DuckDB median 0.01549425s.

Original SF1 Q7 validated 100 rows/5 columns in combined source
90feb7a447f1f2602f5e9e5c1546e2fde8826f41 (v0.0.31-dsfix7),
library SHA256 14d0702d3600d93d9feff4c330ce3094d154f9376882b746152f8a39c1bb07b6.
Warm-up 27.582920387s; timed 22.649948093/22.770575885/22.732100928s;
median 22.732100928s. All timed outputs were fully fetched and checked.

Both used retained original SF1 SQL/data, Redis Software experimental Flex with
verified active 4x QPF, Linux ARM64 client in the server network namespace,
client 4CPU/8GiB and server 20GiB. These combined artifacts include separate
fixes and establish combined completion/correctness, not isolated attribution
to this PR. Full benchmark suites remain stopped; no issue closure is implied.

Current-main clean branch verification: all unit-only packages passed `env -u REDIS_URI GOMAXPROCS=2 go test -count=1 -p 1 ./...` (root package 32.775s). Redis-URI integration skips do not establish live compatibility. Formatting/diff checks pass. Earlier local hold statements above record historical preparation; publication is now authorized.
