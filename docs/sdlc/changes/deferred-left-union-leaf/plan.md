# Plan and verification

Date: 2026-11-23. Public baseline: `760d1c8a6e91391b4ff4ceb13ec1b764fa012bd9`. Local candidate.
See [intent](intent.md), [specification](spec.md).

1. Prove narrow direct-source compound-equality LEFT leaves in the UNION guard.
2. Clone leaf join state per execution and rebuild preserved-left-only pushdown.
3. Validate actual parent deferral, complete residual/plan isolation, duplicate/
   NULL compound keys/site values, no-input, native ownership refusal, and unsafe
   outer/arithmetic/error/volatile fallback using pure fixtures.
4. Parent independently reviews and pilots selected Q5 on backed-up Redis
   Software with verified active 4x QPF; full benchmarks remain stopped.

All unit-only Go packages passed on this isolated public branch:
`env -u REDIS_URI GOMAXPROCS=2 go test -count=1 -p 1 ./...`
(root package 28.261s). Redis-URI integration skips do not establish standalone
native Software compatibility. Formatting and diff checks passed. The six LEFT-focused tests include
actual parent deferral, a bounded join-work test that would exhaust admission
if date filtering occurred only after the join, and no-read published/private/
unpinned native eligibility. These tests use only helpers already on public
main; unpublished streaming/cohort helpers are unnecessary.

No external SQL, Redis endpoints, containers, native library builds, benchmark
runs, or changes to the parent's current live artifacts occurred while preparing
this clean branch. The separate combined-artifact observation below does not
establish isolated LEFT-only native performance.

## Targeted Q5 combined-artifact observation

The original SF1 Q5 fully fetched **100 rows and 5 columns** and independently
validated all three timed outputs against native DuckDB. Combined source:
`119633325dc33d8fd633c3bfea71375ba43b245b`; GetInfo: `v0.0.31-dsfix6`; library
SHA-256: `200168f2dd888982b33353f23ae7faf46cff5bf4c5f784f84a0db13f15fd6249`.

| Measurement | Seconds |
|---|---:|
| Redis warm-up | 8.051566086|
| Redis timed run 1 | 3.463930335|
| Redis timed run 2 | 2.881045418|
| Redis timed run 3 | 3.183502876|
| Redis median | 3.183502876|
| Native DuckDB median | 0.015494250|

Profile: Redis Software 8.8.0 Flex experimental stopgap with verified active 4× QPF,
six Search workers, mnp routing and 32 connections; logical capacity 64 GiB and
Flex cache 8 GiB. Linux ARM64 client in the server network namespace, client 4 CPU/
8 GiB ceiling and Software 20 GiB ceiling. Original SF1 fixture/SQL, one warm-up,
three timed runs, 60-second statement deadlines, isolated sequential engines,
and separate disk-backed result validation. Full suites remain stopped.

This was the parent's combined build, which also contained candidate-cap,
streaming, fact-demand and scalar-cohort work. This clean LEFT PR contains only
the isolated change above; a clean standalone native run would be needed for
isolated attribution. [Q5 #591](https://github.com/redis/redis-adbc-driver/issues/591)
remains open for maintainer review.
