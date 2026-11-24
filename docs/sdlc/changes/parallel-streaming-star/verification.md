# Verification

Date: 2026-11-23. Status: expanded synthetic and race checks passed.
See [intent](intent.md), [specification](spec.md), [plan](plan.md).

Tests use in-memory rows and self-contained native-directory mock transport;
`unused.invalid` is never contacted. They assert actual two-worker grants and
real parsed-plan execution, compare exact decimal/CASE grouped results with the
established serial path, preserve duplicate/NULL/provenance bags and range order,
exercise backpressure, int64 bounds beyond 2^53, source-versus-consumer failures,
cancellation, queued admission cleanup, single-grant cleanup, complete fact
residuals and selective-path refusal. A focused race pass before the final test
additions completed in 3.266s; final checks are recorded after completion.

No Redis endpoint, native library build, SQL query, container action or benchmark
was run. Skipped integration tests are not compatibility or performance evidence.

Original integrated-source focused race command passed (3.309s):

```
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB \
  go test -race -count=1 -p 1 \
  -run '^(TestParallelStar|TestStar|TestUnionStar|TestDeferredUnionStream)' .
```

All packages also passed the same endpoint-free `go test -count=1 -p 1 ./...`
command (root package 28.498s); applicable integration tests skip without Redis
endpoints. Formatting, diff and SDLC local-link checks passed. No native
performance improvement or query resolution is claimed from these checks.

## Clean publication and HASH-read correction

The original PR612 publication was stacked on public [PR607](https://github.com/redis/redis-adbc-driver/pull/607) head `c73e5b66bf45d8c0eb1203171c04b399dbcbc4ca`, containing only bounded parallel fact/probe and shared budget-proof changes plus records/tests. It does not copy scalar cohort production, private integration ancestors, native join-order/fact-demand changes or the companion PR608 cap adjustment. The count/byte proof uses the candidate constants already installed in its base.

The shared HASH-read guard from [#609](https://github.com/redis/redis-adbc-driver/issues/609)/[PR610](https://github.com/redis/redis-adbc-driver/pull/610) is included as a one-line prerequisite because that PR is not yet merged in the selected base. A standalone actual parallel-path transport regression supplies populated/error-free HMGET replies then returns composed Nil plus reply-loss (or wrapped Nil): both must fail without reducer output and release the complete grant. A direct-Nil control retains its established behavior and all duplicate rows. This does not require or import cohort production, and does not claim all other read helpers were audited.

## Targeted Q7/Q4 combined-artifact observation

The separately tested combined source `90feb7a447f1f2602f5e9e5c1546e2fde8826f41`, GetInfo `v0.0.31-dsfix7`, native-library SHA-256 `14d0702d3600d93d9feff4c330ce3094d154f9376882b746152f8a39c1bb07b6`, fully fetched and independently validated original TPC-DS SF1 Q7: **100 rows and five columns**.

| Q7 measurement | Seconds |
|---|---:|
| Redis warm-up | 27.582920387 |
| Redis timed run 1 | 22.649948093 |
| Redis timed run 2 | 22.770575885 |
| Redis timed run 3 | 22.732100928 |
| Redis median | 22.732100928 |
| Native DuckDB median | 0.022591875 |

The same artifact's Q4 still timed out during warm-up at the configured 60-second statement deadline (SQLSTATE 57014), with client OOM-kill delta zero. No Redis timed samples, validated Redis output or median/ratio were recorded for that Q4 attempt. The native DuckDB control passed (median 0.048231709 seconds).

Profile: Redis Software 8.8.0 Flex experimental stopgap with active verified 4× QPF, six Search workers, mnp routing and 32 connections; logical capacity 64 GiB/Flex RAM cache 8 GiB. Linux ARM64 client in the server network namespace, four client CPUs and an 8 GiB client memory ceiling, Software server ceiling 20 GiB. Original SF1 fixture/SQL, one warm-up plus three fully fetched and independently validated timed outputs, configured 60-second statement deadlines, sequential native DuckDB/direct Redis processes and separate disk-backed validation. Full benchmark suites remain stopped.

The live combined source additionally included fact-demand HASH-list fixes, native join ordering and cohort work. The measured Q7 result is not attributed solely to this public head, the parallel reader, the separate cap constant or any one PR. A clean standalone rebuilt/live test is still required for isolated performance attribution. [Q7 #593](https://github.com/redis/redis-adbc-driver/issues/593) and [Q4 #590](https://github.com/redis/redis-adbc-driver/issues/590) remain open; no automatic closure is requested. This publication task performed no live query, library build or container operation.

## Clean public-branch checks

The original PR612 public branch retained the then-installed 65,536-candidate cap. Its byte-boundary
fixture uses longer synthetic prefixes and explicitly asserts that the byte
threshold is reached before the count cap. The earlier short-prefix fixture
correctly hit the lower count cap first; no production budget or assertion was
weakened.

Endpoint-free focused race checks including the standalone HASH transport
regression and count/byte boundaries passed (4.205s):

```
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB \
  go test -C go -p 1 -race -count=1 \
  -run '^(TestParallelStar|TestFlexComplete|TestCompleteFlex|TestCompleteSnapshot|TestSnapshotPage|TestStar|TestUnionStar|TestDeferredUnionStream)' .
```

All endpoint-free packages passed on the clean public branch using
`env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB go test -C go -p 1 -count=1 ./...`
(root package 28.587s). Integration skips without an endpoint are expected;
these checks do not establish Redis Software compatibility. Formatting and
`git diff --check` passed. The earlier integrated-source timings above are
historical checks, separate from this public-branch verification.

Published for human review as [PR612](https://github.com/redis/redis-adbc-driver/pull/612), stacked on PR607.
