# Validation, scope and live context

Date: 2026-11-23. Public source baseline: `07b24edb3c782037f48670f6cc499d66256a59e0`.
See [intent](intent.md), [specification](spec.md), [plan](plan.md).
Primary issue: [TPC-DS Q3 #589](https://github.com/redis/redis-adbc-driver/issues/589).

## Publication boundaries

The clean publication contains `query.go`, `parallel_aggregate.go`, `snapshot_candidates.go`, `snapshot_delta.go`, `snapshot_page_filter.go`, `snapshot_page_filter_test.go`, `snapshot_flex_candidates.go`, `snapshot_flex_candidates_test.go`, and the two Flex change-record directories. It does not contain CTE binding, join/view/lateral behavior, UNION demand, scalar residual optimization, benchmark SQL rewrites or storage-format changes. The branch is reconstructed directly on the public baseline; it does not publish the combined investigation history.

Complete bounded candidates are the production route. Over-budget, broad and ineligible sources retain original exact-directory scanning without per-page Search probes. Search determines only complete predicate candidates; exact directories resolve visible winners, and authoritative HASH reads plus the original SQL residual remain necessary.

## Combined-artifact Q3 observation

A separately tested combined artifact (`GetInfo v0.0.31-dsfix2`, native-library SHA-256 `da4177d6077a20b87bf591a31c0873afe5f4fd238c370969776c7b0f997cc6d5`) included these Flex changes **and** separate lazy CTE/join and deferred UNION changes. The original TPC-DS SF1 Q3 returned 89 rows and four columns; all timed outputs were fully fetched and independently validated against a native DuckDB control.

| Measurement | Seconds |
|---|---:|
| Redis warm-up | 4.614876835 |
| Redis timed run 1 | 3.211921584 |
| Redis timed run 2 | 3.162769252 |
| Redis timed run 3 | 3.103696251 |
| Redis median | 3.162769252 |
| Native DuckDB median | 0.006819083 |

Profile: Redis Software 8.8.0 Flex experimental stopgap; active verified 4× QPF, six Search workers, mnp routing and 32 connections. Linux ARM64 client in the server network namespace, four client CPUs and 8 GiB client memory ceiling. Software container ceiling 20 GiB; logical database capacity 64 GiB with 8 GiB Flex RAM cache. Original SF1 fixture and SQL were retained. One warm-up and three timed runs per engine, configured 60-second statement deadlines, sequential fresh engine processes and separate disk-backed validation. Full benchmark suites stayed stopped.

This is a **combined-artifact observation**, not a pure-Flex SF1 result. A separate clean Flex build/live retest is still required for that attribution. The preceding combined candidate still timed out on Q3; this progression motivates the bounded candidate route but does not remove the attribution limit. Q4/Q5/Q9 and other issue outcomes cannot be inferred from Q3. No issue is closed by this evidence.

## Focused correctness and remaining review

The mocked-transport tests independently exercise Flex candidate completeness, exact source membership, zero-score directory checks, int64 ordering beyond 2^53, newer-version/tombstone shadowing, HASH identity, byte/count bounds, cancellation/lease checks, original residual/field mapping, warning/reply corruption and ACL/transport failure handling. A confirmed direct native result-limit refusal can fall back; composed/uncertain errors cannot. These source-focused tests do not invoke real Redis or establish benchmark latency.

Local verification on the clean Flex-only branch passed:

```sh
env -u REDIS_URI GOMAXPROCS=2 go test -p 1 -count=1 ./...
```

All Go packages passed; root package test runtime was 28.355 seconds. Real Redis integration tests were not run; applicable integration skips with `REDIS_URI` unset are not compatibility evidence. `git diff --check` and local change-record links passed. The extracted Go files match the reviewed combined artifact byte for byte, and standalone compilation/tests establish that no omitted join/CTE/UNION source is a compile dependency. Publication CI results are recorded on the PR. Human maintainer review remains pending. No live SQL/container runs are performed by the publication task.
