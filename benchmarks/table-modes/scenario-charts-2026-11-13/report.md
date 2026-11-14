# Scenario timings and chart data — November 13, 2026

All **168 standard trial records** validated: 96 OLTP and 72 OLAP, including 42 warmups and 126 measured trials. Each measured point case has 96 operation observations (3×32); each query case has 24 (3×8). All 84 smaller pilot records also validated. No failed results, conflicts, automatic retries or discarded samples. This adds measurements to the existing 156-trial SQL/Arrow/native comparison; it does not rerun 100M ClickBench.

## Point operations

One known record per call, a 1,000-row/four-column fixture, persistent per-thread clients. Read returns the same typed Arrow row. INSERT/UPDATE/DELETE validate the complete resulting fixture. P50/p95 pool operation latencies across measured trials. Closed-loop fixed-operation throughput includes synchronization/scheduling and is not a saturated capacity score.

| Operation | Mode | Clients | P50 ms | P95 ms | Median ops/s | Samples |
|---|---|---:|---:|---:|---:|---:|
| delete | managed | 1 | 63.656 | 122.141 | 14.8 | 96 |
| delete | managed | 4 | 175.397 | 736.850 | 12.7 | 96 |
| delete | redis_py | 1 | 0.234 | 0.315 | 2907.5 | 96 |
| delete | redis_py | 4 | 0.405 | 5.809 | 3679.0 | 96 |
| delete | unmanaged | 1 | 9.924 | 14.457 | 96.2 | 96 |
| delete | unmanaged | 4 | 16.855 | 26.657 | 222.4 | 96 |
| insert | managed | 1 | 65.754 | 110.354 | 14.5 | 96 |
| insert | managed | 4 | 105.990 | 1265.923 | 10.2 | 96 |
| insert | redis_py | 1 | 0.248 | 0.331 | 2856.7 | 96 |
| insert | redis_py | 4 | 0.435 | 6.321 | 3385.7 | 96 |
| insert | unmanaged | 1 | 9.952 | 12.530 | 96.7 | 96 |
| insert | unmanaged | 4 | 16.552 | 25.719 | 220.9 | 96 |
| point_read | managed | 1 | 8.479 | 11.683 | 113.6 | 96 |
| point_read | managed | 4 | 13.558 | 18.839 | 273.7 | 96 |
| point_read | redis_py | 1 | 0.255 | 0.463 | 2762.6 | 96 |
| point_read | redis_py | 4 | 0.478 | 6.012 | 3328.1 | 96 |
| point_read | unmanaged | 1 | 8.804 | 10.518 | 109.9 | 96 |
| point_read | unmanaged | 4 | 13.567 | 19.228 | 274.9 | 96 |
| update | managed | 1 | 84.125 | 114.557 | 12.6 | 96 |
| update | managed | 4 | 142.967 | 1014.398 | 11.7 | 96 |
| update | redis_py | 1 | 0.259 | 2.781 | 2200.6 | 96 |
| update | redis_py | 4 | 0.404 | 5.984 | 3475.5 | 96 |
| update | unmanaged | 1 | 9.976 | 15.288 | 94.2 | 96 |
| update | unmanaged | 4 | 16.725 | 22.930 | 230.0 | 96 |

## Filter and aggregate queries

100,000 rows/four typed columns. Filter projection returns 100 rows; filtered COUNT/SUM processes 10,000 matching rows; grouped COUNT/SUM returns 16 groups. SQL COUNT/SUM outputs are explicitly cast to BIGINT for matching int64 Arrow types; exact SQL is in the measured harness. Redis client uses Search cursor discovery + HMGET for projection and Search reducers for aggregates, plus typed Arrow conversion. Equivalent output does not imply identical planner/index execution or guarantees.

| Query | Mode | Clients | P50 ms | P95 ms | Median queries/s | Samples |
|---|---|---:|---:|---:|---:|---:|
| filtered_count_sum | managed | 1 | 49.171 | 70.371 | 19.4 | 24 |
| filtered_count_sum | managed | 4 | 97.604 | 114.018 | 40.9 | 24 |
| filtered_count_sum | redis_py | 1 | 3.063 | 6.924 | 276.0 | 24 |
| filtered_count_sum | redis_py | 4 | 10.917 | 13.661 | 369.1 | 24 |
| filtered_count_sum | unmanaged | 1 | 262.624 | 276.731 | 3.7 | 24 |
| filtered_count_sum | unmanaged | 4 | 173.424 | 213.249 | 22.8 | 24 |
| filtered_projection | managed | 1 | 25.336 | 29.442 | 42.4 | 24 |
| filtered_projection | managed | 4 | 18.362 | 30.081 | 192.5 | 24 |
| filtered_projection | redis_py | 1 | 1.151 | 4.512 | 639.5 | 24 |
| filtered_projection | redis_py | 4 | 5.053 | 8.459 | 748.3 | 24 |
| filtered_projection | unmanaged | 1 | 16.920 | 29.036 | 47.3 | 24 |
| filtered_projection | unmanaged | 4 | 30.817 | 45.677 | 142.7 | 24 |
| grouped_count_sum | managed | 1 | 550.449 | 598.521 | 1.8 | 24 |
| grouped_count_sum | managed | 4 | 775.820 | 849.966 | 5.0 | 24 |
| grouped_count_sum | redis_py | 1 | 35.898 | 41.599 | 27.3 | 24 |
| grouped_count_sum | redis_py | 4 | 107.569 | 185.035 | 36.4 | 24 |
| grouped_count_sum | unmanaged | 1 | 402.906 | 419.505 | 2.5 | 24 |
| grouped_count_sum | unmanaged | 4 | 535.830 | 591.367 | 7.2 | 24 |

## Chart exports

[charts.json](charts.json) and [charts.csv](charts.csv) contain **84 current chart rows**, explicit units, scenario/client counts, fixture sizes, sample counts and driver/redis-py elapsed multiples. Below 1× means less elapsed time. Series cover OLTP, SQL/Arrow ingestion, full scans, filters/aggregates, transfers and a representative numeric slide-4 overview. They join this run to the [156-trial dataset](../merged-scoped-2026-11-13/measurements.json) only after exact driver source/version/binary identity checks. Distinct timing scopes and four-column versus 105-column fixtures remain labeled. The existing bulk data have 10K total rows; full scans have 100K rows.

Historical [ClickBench CSV](clickbench-historical.csv) and [paired aggregate CSV](aggregate-historical.csv) remain separate: October 21 Redis Open Source, unequal cross-product hardware and hot minima in the first; separate within-Redis builds and three-round medians in the second. JSON carries the same historical distinctions. No current Software or product-wide ranking follows from these archived series.

Regenerate with:

```sh
python3 benchmarks/table-modes/chart_data.py \
  --table-modes benchmarks/table-modes/merged-scoped-2026-11-13/measurements.json \
  --scenarios benchmarks/table-modes/scenario-charts-2026-11-13/measurements.json \
  --historical-clickbench benchmarks/clickbench/full-c6a-cluster-2026-10-21/early-comparison/comparison.json \
  --historical-aggregates benchmarks/exact-aggregate-406/2026-10-21/full/summary.json \
  --out /private/new-chart-directory
```

The exporter is offline: it verifies coverage, repetitions, validation flags, arithmetic and compatible driver builds; it never contacts Redis or launches ClickBench. Every input SHA256 is recorded in charts.json. Use the bundle audit described below to check exact regenerated content.

## Build, environment and boundaries

Driver source `ca6858065c4cf02418d55c3aa62eb583590fe252`, version `v0.1.0-dev.merged-scoped-1`, SHA256 `8d358ce5d2a9adbf06a6f1b0fe381e57563596b206e620ee368c4948254084f9`. Same exact versioned Go 1.27.3 binary as the prior table-mode run. Measured scenario harness: commit `cebdc45f0a093fffb64ee7698b5d870fe2b7a11a`, SHA256 `7e0ef8ab173b264287d2644b1d0485eae91970b7c3cce92777a485c8331da36a`. Subsequent failure-retention and chart validation guards are tested separately; the observations retain their actual measured harness identity. Per-operation action/query/timer bodies were unchanged by that follow-up.

Actual Redis Software 8.2.6-25 / Redis 8.8.0 / Search 80808; active/effective 4× QPF, six Search workers, mnp/32, one 4-GiB noeviction RAM shard, actual 4-CPU/8-GiB container limits. Certificate/hostname-verified data TLS and RESP3. Existing management bootstrap uses its self-signed trust mode. Apple M5 Max; 18 logical CPUs; 36 GiB; macOS 26.7.3 arm64. Client CPU/RAM are uncapped; GOMAXPROCS/GOGC/GOMEMLIMIT/GODEBUG unset. Fixed recorded local server image, not a registry-digest-pinned tag.

Known-address reads exclude key lookup: managed SQL uses actual internal row IDs obtained before timing; unmanaged/raw use application HASH keys. The snapshot-aware NativeClient is not measured. All operations use autocommit; multi-statement transactions are not provided. Unmanaged INSERT refuses an existing key; raw HSET can overwrite. Managed mutations publish snapshots and serialize admission; raw client commands do not provide those semantics.

Connection setup/teardown, fixture preparation, complete post-state and independent type/value/count validation are excluded from scenario latency/windows. Every timed query fully consumes the typed Arrow result. Prior bulk/scan wall times include fresh worker startup/imports/connections/IPC and shutdown, as recorded by that harness; do not compare those seconds directly with persistent point-operation milliseconds.

Case order alternates by round; read-only OLAP cases share an immutable per-mode/round fixture. Synthetic int64/string data have no NULLs or floating-point edge cases. These finite samples do not establish confidence intervals, production capacity or SLA tail latency; caches, host scheduling and thermal effects can matter. All other heavy benchmark/render work paused during native timing.

## Verification and cleanup

Audit the bundle with `python3 benchmarks/table-modes/audit_scenario_charts.py`. Unit checks cover exact independent group arithmetic, unique mutation targets, cursor cleanup without replay, failed-worker cancellation, retention of failed timed samples, rejection of missing/duplicate/failed/miscomputed chart records, compatible future driver identities, and historical series separation.

The complete native pilot and standard matrices validated every returned typed value/multiplicity and complete point-mutation post-state. All worker/client scopes ended before the owner verified empty/index-free data and rechecked QPF/TLS/RESP3. Teardown removed the owned container/network and generated credentials/TLS, preserving preexisting resources; [cleanup proof](cleanup.json) and [provenance](provenance.json) retain sanitized evidence. The generated-credential scan found zero matches. Endpoint/infrastructure identities and operational logs remain private.
