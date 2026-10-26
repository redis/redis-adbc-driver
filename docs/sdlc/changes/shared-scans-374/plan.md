# Plan and evidence

2026-10-21; baseline `95d6365`; author Codex for alberttwong.
Issue [#374](https://github.com/alberttwong/redis-adbc-driver/issues/374).
See [intent](intent.md), [spec](spec.md). Status: implemented and locally validated; awaiting maintainer review.

1. Share reader construction, owner registration and internal range dispatch.
2. Admit eligible lowered-cap ordinary scans, retaining index predicates and
   global ordering/limit placement. Retain complete existing fallbacks elsewhere.
3. Fix RESP2 CONFIG decoding uncovered by fresh-cap regressions; check released
   prefix identity between ranges.
4. Compare exact values/ordering/types against the old HASH path; run lifecycle,
   public native API, standalone/Cluster, real lowered-cap and race regressions.
5. Record command/latency evidence, document tradeoffs, and open a PR with
   `Closes #374`. The issue closes on merge, not merely on PR creation.

## Verification

Local environment: macOS 26.7.1 arm64, Go 1.27.2,
Redis OSS 8.6.2/Search 8.6.0; standalone localhost:6379, local OSS Cluster API
localhost:7001, and temporary standalone 127.0.0.1:16390 with real aggregate cap
137. Python 3.13.16, Arrow 26.0.0, ADBC driver-manager 1.12.0.

## Tradeoffs and remaining limits

Range scans bound client buffering and avoid a whole-result key deduplication
set for eligible streaming reads, but can make more Redis requests and sort
per range. Tiny low-cap scans can be slower than materialized HASH reads.
No throughput win or measured peak-memory claim is made. Cloud/Software and
older macOS validation from #363 remain separate work. No paid resources are
provisioned. The temporary local test container is task-owned.

## Completed checks

- Full Go integration, `REDIS_URI=redis://localhost:6379/0 go test -count=1 -p 1 ./...`
  from `go/`: all three packages passed (main 86.359s).
- Focused race, `go test -race -count=1 -p 1 -run '^(TestSharedScan|TestPartition|TestSQLStreamResults)' .`
  against localhost:6379: passed, 8.282s.
- Focused local OSS Cluster, `go test -count=1 -p 1 -run '^(TestSharedScan|TestPartition)' .`
  against localhost:7001: passed, 1.674s.
- Real aggregate cap 137, `go test -count=1 -p 1 -run '^TestSharedScan' -v .`
  against 127.0.0.1:16390: passed, 1.085s.
- The race, Cluster and cap-137 focused runs preceded the additional 25,000-row
  multi-page cursor test; the final full Go run includes that test.
- Native library build: passed. Full Python ADBC validation against cap 137:
  369 passed, 8 feature skips, 3 subtests passed, 7.76s. Skips cover existing
  unsupported transaction/statistics/secondary catalog/connection/ingest features.
- `git diff --check`: passed.

The native cancellation regression retains row count, cancellation SQLSTATE,
statement reuse and cleanup assertions. Its server-cursor expectation now
accounts for small cap windows that fit in one cursor page: a live logical
reader may have no open server cursor between ranges.

## Focused measurement

`TestSharedScanMeasurement` reads and checks all 5,000 rows in exact insertion
order against the old forced HASH fallback. One local cap-137 sample:

| Path | Full fetch | Commands |
|---|---:|---|
| Bounded | 54.502458ms | 38 CONFIG, 37 FT.AGGREGATE, 37 FT.SEARCH, 1 GET, 72 HGET, 5,000 HMGET, 36 SISMEMBER |
| HASH fallback | 12.507417ms | 1 CONFIG, 1 GET, 5,000 HMGET, 6 SCAN |

This is a focused regression measurement, not a production benchmark. The
bounded path was slower with this tiny cap; it removes whole-result key
deduplication and bounds streaming buffers. Peak memory was not measured.
ClickBench trials were stopped before execution and are excluded from this
refactor. The unfinished local comparison harness is not part of this change.
