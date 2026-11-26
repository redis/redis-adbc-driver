# Validation and review

Date: 2026-11-25. Baseline: `c8f6f50d`, rebased onto `844d8123` (#689).
Issue: [#627](https://github.com/redis/redis-adbc-driver/issues/627). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Environment

Local Docker Desktop on Apple silicon. Redis Software **8.2.6-58**: the newest build in the Redis Software release notes, and the cached `redislabs/redis:latest`. Redis 8.8.0, Search 8.8.8. 4× QPF was verified by the fixture scripts: active, scaling factor 4, 6 Search workers, `mnp` routing, 32 connections. Databases:

- RAM: 3 shards, 4 GiB.
- Flex: 1 shard, BigStore v2, 4,096 MB with 512 MB in RAM (the 12.5% RAM share of #627's 64 GiB / 8 GiB).

Data was TPC-DS SF 0.1 from DuckDB 2.0.1 `dsdgen`, loaded through the driver, with row counts checked; Flex held Q4's five tables. The client was macOS, connecting through Docker's published port. The DuckDB-over-ADBC path used the harness's `connect_engine` and Query.Farm's released `adbc_scanner` `98ff0a39fc`, with `query_pushdown false`, `scan_threads` 1 and 4 DuckDB threads. Every run did one warm-up and three timed runs with a full fetch, compared with native DuckDB as row multisets.

This is not #627's SF1 profile. Its experimental parallel scanner was not available.

## Diagnosis on the baseline

| Q4, median of timed runs | RAM | Flex |
|---|---:|---:|
| Native DuckDB | 0.021 s | 0.018 s |
| Direct Redis ADBC | 6.9 s | 31.2 s |
| DuckDB over ADBC | 4.28 s | 44.3 s (55.1 s warm-up) |

On RAM, ADBC table scans were 99% of DuckDB operator time. On Flex, 33.6 s of the 55 s warm-up was DuckDB binding. While binding each of Q4's nine table references, the scanner calls approximate `GetStatistics` and caches nothing (`TryGetStatisticsFromADBC` in the scanner's `src/adbc_scan.cpp`). On Flex those calls read every row ID:

| Approximate `GetStatistics` | Flex | RAM | Flex `COUNT(*)` |
|---|---:|---:|---:|
| `store_sales` (288,464 rows) | 5.62 s | 0.19 s | 2.94 s |
| `catalog_sales` (143,657) | 8.93 s | 0.10 s | 1.40 s |
| `web_sales` (71,632) | 4.44 s | 0.07 s | 0.93 s |
| `date_dim` (73,049) | 3.85 s | 0.12 s | 0.91 s |
| `customer` (10,000) | 0.56 s | 0.03 s | 0.12 s |

## Flex capability retest (Software 8.2.6-58)

Direct probes on a scratch index, and on the driver's flash-backed `store_sales` snapshot index:

- **Accepted:** plain NUMERIC/TAG indexes with `SKIPINITIALSCAN`; TAG `INDEXEMPTY` at creation (its compaction correctness, #573, was not tested); `FT.AGGREGATE` with `LOAD` or `SORTBY`.
- **Counts:** `FT.SEARCH … NOCONTENT LIMIT 0 0` counts were exact. The visibility queries `-@__adbc_snapshot_deleted:[1 1]`, the same with one masked range, and with three masked ranges returned 288,464, 288,454 and 286,989, as expected. Each took about 0.6–0.8 s net.
- **Sorted paging:** `SORTBY __rowid ASC/DESC` works on the non-`SORTABLE` NUMERIC row ID. Keyset paging all 288,464 IDs in 10,000-row pages returned every ID once, in order, but took 47 s, with the first page alone at 7 s. Its first page also needed `TIMEOUT 0`.
- **Still rejected:** `NUMERIC SORTABLE` ("Disk index does not support SORTABLE fields"), `WITHCURSOR`, `FT.ALTER` and `FT.DROPINDEX … DD`.

## With the change

Approximate `GetStatistics` on the same Flex data, with values equal to `COUNT(*)`:

| Table | Before | After |
|---|---:|---:|
| `store_sales` | 4.13 s | 0.71 s |
| `catalog_sales` | 7.62 s | 2.78 s at first, 0.37–0.68 s on repeat |
| `web_sales` | 3.95 s | 0.45 s |
| `customer` | 0.55 s | 0.07 s |
| `date_dim` | 3.50 s | 0.80 s |

Q4 DuckDB over ADBC on Flex, alternating the `main` and change libraries twice on the same data. Every run matched native DuckDB:

| Build | Warm-up | Timed runs (median) | Binding per run |
|---|---|---|---|
| `main` | 92.2 / 56.3 s | 42.4, 39.2, 40.7 (40.7) / 45.2, 38.9, 38.6 (38.9) s | 72.9 s first, then 20.4–24.2 s |
| change | 33.1 / 33.3 s | 31.9, 34.4, 30.3 (31.9) / 28.7, 30.5, 29.1 (29.1) s | 2.1–2.9 s |

What remains is Flex scan time. In an earlier session, a timed run (34.8 s) was slower than its warm-up (24.3 s); profiling every run showed binding at 3.4–6.7 s throughout, with the `store_sales` scan varying from 12.8 s to 31.8 s. Two measurements explain the scan time:

- **Concurrent scans don't add throughput on one Flex shard.** Reading the three fact projections alone took 9.19, 7.79 and 4.97 s (sum 21.95 s), and concurrently 20.92 s of wall time. On RAM, concurrency cut 8.1 s to 5.6 s.
- **The shard's main thread is the limit.** During a single-stream scan it was 0.35–1.29 cores busy, with bursts of about 8,800 flash fetches per second.

Driver partitions of `store_sales` (1 / 4 / 16):
- Flex: 3.98 / 2.76 / 2.83 s.
- RAM, 3 shards: 3.82 / 1.63 / 1.20 s.
- RAM, 1 shard: 2.99 / 1.16 / 0.96 s.

So parallel scanner readers can help on RAM, but barely on one Flex shard.

## Tests

- `TestSnapshotFlexApproximateStatisticsSearchCounts` runs on Flex. It does UPDATE, DELETE and INSERT, then checks overlays and post-compaction counts.
  - It failed on the baseline in both subtests: the counts were correct, but statistics read every row ID instead of counting with Search.
  - It passes with the change, before and after the rebase.
- `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages OK, before and after the rebase.
- Flex: `REDIS_SNAPSHOT_URI=… REDIS_FLEX_URI=… go test -race -count=1 -p 1 -v -run '^(TestFlex|TestSnapshotFlex)' .`, before the rebase:
  - 39 top-level tests passed (111 passing entries), 5 skipped (opt-in profile/failover/restart fixtures) and 1 failed.
  - The failure was `TestFlexReadRobustness/timeout`. It also fails 3 of 3 with the baseline files. Its 50 ms timeout setting first fails `ExecuteQuery` with a write timeout through Docker's published port, before the injected read stall.
- After the rebase onto #689, on a fresh Flex fixture (same settings):
  - The new test passed: the test and both subtests. With the baseline `statistics.go`, `snapshot_visibility.go` and `snapshot_candidates.go` overlaid, all three failed.
  - The Flex suites passed 40 top-level tests (112 passing entries), skipped 5 and failed 1, the same pre-existing `TestFlexReadRobustness/timeout`.
- `make -C go test-software` on a fresh 3-shard RAM fixture with 4× QPF verified, after the rebase: all packages OK; 1,589 passed, 444 skipped, 0 failed.

## Limits

These are SF 0.1 single-shard Flex and 3-shard RAM measurements from a macOS client through Docker's published port, with the released single-reader scanner. The SF1 run and the experimental parallel scanner were not run. Scans on Flex remain server-bound, and the scanner still requests statistics once per table reference. Human review is pending.
