# Performance benchmarks

Measured 2026-11-23: Redis ADBC **v0.0.31** versus DuckDB **v2.0.1** at scale factor 1.
These were the latest stable releases when checked. This is an informal local
end-to-end query comparison, not an audited TPC result.

**Stopped at the user's request.** SSB and TPC-H RAM completed their full query lists.
TPC-DS completed Q1–Q9: Q1 validated, Q2–Q7 and Q9 timed out, and Q8 was
OOM-killed in the client. Q10 was interrupted by the stop request; Q11–Q99
were not run on Redis. The planned TPC-H Flex follow-up was cancelled before execution.
The TPC-DS ratio below represents Q1 alone; it is not a suite score.

Each successful query has one warm-up and three fully fetched, validated timed runs.
Ratios use median latency: values above 1 mean Redis took longer. Totals and
geometric means cover only queries with all three valid timed runs in both engines.
They are not full-suite scores when coverage is incomplete.

**Memory-isolation limitation:** each query worker ran DuckDB before Redis and
kept its DuckDB connection open. Retained DuckDB state and reference results shared
the client memory cap with the Redis driver. The five OOMs are combined-worker
failures during Redis execution, not isolated proof of Redis-driver memory needs.
Memory pressure may also affect timings. Later focused follow-ups use separate engine processes; their results remain separate below.

| Suite | Redis deployment | Validated / expected | Timeouts | Process failures | Other failures | Interrupted / not run | Geomean Redis / DuckDB |
|---|---|---:|---:|---:|---:|---:|---:|
| SSB | Software RAM | 13/13 | 0 | 0 | 0 | 0 / 0 | 361.62× |
| TPC-H (RAM) | Software RAM | 12/22 | 6 | 4 | 0 | 0 / 0 | 577.80× |
| TPC-DS (Flex) | Software Flex (experimental) | 1/99 | 7 | 1 | 0 | 1 / 89 | 714.39× |

Totals below sum the per-query medians on the validated subset only.

| Suite | DuckDB total (s) | Redis total (s) | Redis / DuckDB total |
|---|---:|---:|---:|
| SSB | 0.126915 | 150.457411 | 1185.50× |
| TPC-H (RAM) | 0.214774 | 183.557391 | 854.66× |
| TPC-DS (Flex) | 0.013726 | 9.805355 | 714.39× |

## Environment and method

The checksum-verified Linux ARM64 release wheel supplied the Redis driver;
GetInfo reported v0.0.31 and its native library matched the wheel's SHA-256.
No working-tree driver build or SQL rewrite was used. Tables use the default
managed mode; Flex profiles explicitly enable `adbc.redis.flex=true`.

Redis Software 8.2.6-58 / Redis 8.8.0 had active, verified **4× QPF**, six Search
workers, `mnp` routing, 32 connections and `noeviction`. Each final deployment
used one shard and authenticated TLS. The Linux client shared its server's
network namespace, avoiding the macOS Docker published-port route.

Host: Apple M5 Max, 18 logical CPUs and 36 GiB RAM. Docker VM: 16 CPUs and
31.46 GiB RAM, shared with existing development workloads. The client had a
four-CPU quota; DuckDB used four threads and an 8 GB internal memory limit.
Redis server CPU work was additional to the client quota. These are system
comparisons, not equal-total-CPU engine comparisons.

| Suite | Client RAM limit | Redis database capacity | Redis server container RAM limit |
|---|---:|---|---:|
| SSB | 12 GiB | 12 GiB RAM | Docker VM capacity |
| TPC-H | 8 GiB | 18 GiB RAM | 20 GiB |
| TPC-DS | 8 GiB | 64 GiB total / 8 GiB RAM, local disk | 20 GiB |

Swap was disabled for the final clients. Compare engines within a suite:
the suites use different memory budgets, and TPC-DS uses disk-backed Flex.
The Flex storage is local Docker storage, not a separately provisioned SSD service.

The final Flex profile uses Search disk memory/write-buffer percentages 8/2
and a 3 MiB per-index buffer, verified alongside QPF before loading.

Each source dataset was generated once and supplied to both engines. All
loaded tables passed independent row-count checks before query execution.
Loading is excluded from query timings. Each query process performs one warm-up
and three timed runs, including complete result fetch; result comparison is
outside the timer. Both engines have a 60-second SQL statement deadline.
An isolated query process has a 510-second overall ceiling, allowing all eight
statements plus setup and cleanup; a native process failure does not stop later queries.

Validation compares complete row multisets, preserving duplicates, using
1e-7 relative / 1e-6 absolute numeric tolerance. Output ordering and Arrow type
identity are not checked. A ratio requires all three timed runs to match
DuckDB's complete warm-up result. Failed or partial repetitions never enter a ratio.

SSB uses upstream ssb-dbgen data and ssb-tools SQL. TPC-H and TPC-DS use the
unmodified SQL exported by DuckDB 2.0.1; its TPC-DS generator follows TPC-DS v4.

### Earlier attempts and limits

An initial 4 GiB SSB client completed Q1.1–Q1.3 before an OOM kill during Q2.1.
Those samples remain labeled in the preliminary archive and are excluded here.
The final SSB run reused the verified identical data with a larger client.
Four TPC-H query workers were OOM-killed under the 8 GiB client limit; the
server recorded no OOM kills. Timeouts and memory failures are resource-bounded
outcomes, not claims that the queries can never complete with larger budgets.

The initial TPC-DS RAM setup was refused for insufficient memory before
loading or query execution. The user then selected Software Flex. Its released
driver support, typed NULL/date/decimal reads and writes, aggregation and effective
4× QPF were verified before bulk loading. A separate DuckDB-only control completed
all 99 TPC-DS queries successfully. RAM and Flex attempts remain separately labeled.

An initial Flex load used a 12 GiB server limit and default Search disk memory/
write-buffer percentages 60/20. The server was OOM-killed during inventory index
publication; the driver refused publication with `final index directory changed`.
No queries ran on that incomplete load. The final fresh retry increased server
headroom and reduced Search cache budgets without changing SQL, data or indexed columns.

The fresh retry initially used server16/client12 GiB. Inventory indexing's
working memory required a further change to server20/client8 GiB, completed
before any query timing. Intermediate resource observations are retained.

### Evidence and reproduction

- [Sanitized historical samples and provenance](docs/performance/release-sf1-2026-11-23.json)
- [Read-only statistics verifier](benchmarks/olap-release/summarize_baseline.py)
- [Redis v0.0.31 release](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.31)
- [DuckDB v2.0.1 release](https://github.com/duckdb/duckdb/releases/tag/v2.0.1)
- [Pinned SSB generator](https://github.com/electrum/ssb-dbgen/tree/219403ad7d1dd32ae1f97b5553abf92129fccd7f)
- [Pinned SSB schema and SQL](https://github.com/eyalroz/ssb-tools/tree/1414741686b32bbd70c250a4056ebf38866e6a84)

The JSON preserves all recorded timed samples, outcomes, issue links and SQL
checksums through an explicit field allowlist. Failed-query shapes are labeled
as reference dimensions. Failed Redis attempts supply no successful latency or
ratio. Raw operational logs, metadata, endpoints, process/infrastructure
identifiers, credentials and license files remain private. Archive hashes identify
the original retained query records; publishing a sanitized derivative does not
rewrite them.

Recompute medians, matched-subset totals and geometric means without running SQL:

```sh
python3 benchmarks/olap-release/summarize_baseline.py --check
```

The verifier reads only the fixed historical JSON and this report; it never
modifies evidence or discovers later candidate directories. Current benchmark
[runner](benchmarks/olap-release/run.py) and [isolation implementation](benchmarks/olap-release/isolation.py)
include later fixes and therefore do not reproduce the historical shared-worker
process arrangement. For a newly authorized reproduction, restore verified SF1
data, verify Redis Software 4× QPF and the recorded releases/settings, and use a
new output directory. No full-suite rerun is authorized by this report.

## SSB

Deployment: Software RAM. Fully validated: 13/13.

| Query | DuckDB median (ms) | Redis median (ms) | Redis / DuckDB | Status |
|---|---:|---:|---:|---|
| 1.1 | 9.846 | 1534.638 | 155.86× | validated |
| 1.2 | 7.307 | 717.238 | 98.16× | validated |
| 1.3 | 4.641 | 528.767 | 113.94× | validated |
| 2.1 | 8.926 | 20847.293 | 2335.48× | validated |
| 2.2 | 12.074 | 31379.227 | 2598.80× | validated |
| 2.3 | 8.151 | 250.178 | 30.69× | validated |
| 3.1 | 11.152 | 29389.808 | 2635.48× | validated |
| 3.2 | 7.589 | 1873.856 | 246.90× | validated |
| 3.3 | 6.636 | 577.468 | 87.02× | validated |
| 3.4 | 5.136 | 588.390 | 114.56× | validated |
| 4.1 | 10.957 | 32052.490 | 2925.39× | validated |
| 4.2 | 14.164 | 27662.159 | 1952.98× | validated |
| 4.3 | 20.336 | 3055.898 | 150.27× | validated |

## TPC-H (RAM)

Deployment: Software RAM. Fully validated: 12/22.

| Query | DuckDB median (ms) | Redis median (ms) | Redis / DuckDB | Status |
|---|---:|---:|---:|---|
| 1 | 18.457 | — | — | timeout ([#578](https://github.com/redis/redis-adbc-driver/issues/578)) |
| 2 | 4.917 | 11619.746 | 2363.16× | validated |
| 3 | 9.516 | — | — | timeout ([#579](https://github.com/redis/redis-adbc-driver/issues/579)) |
| 4 | 15.881 | — | — | timeout ([#580](https://github.com/redis/redis-adbc-driver/issues/580)) |
| 5 | 14.517 | — | — | process failure ([#581](https://github.com/redis/redis-adbc-driver/issues/581)) |
| 6 | 7.659 | 5053.493 | 659.80× | validated |
| 7 | 15.038 | — | — | process failure ([#582](https://github.com/redis/redis-adbc-driver/issues/582)) |
| 8 | 17.472 | — | — | process failure ([#583](https://github.com/redis/redis-adbc-driver/issues/583)) |
| 9 | 33.007 | — | — | process failure ([#584](https://github.com/redis/redis-adbc-driver/issues/584)) |
| 10 | 40.967 | 37691.192 | 920.03× | validated |
| 11 | 8.008 | 882.335 | 110.18× | validated |
| 12 | 12.376 | 30904.746 | 2497.15× | validated |
| 13 | 51.945 | 28835.769 | 555.12× | validated |
| 14 | 13.981 | 4015.024 | 287.18× | validated |
| 15 | 12.606 | 6465.459 | 512.87× | validated |
| 16 | 21.716 | 6880.728 | 316.85× | validated |
| 17 | 11.985 | 704.473 | 58.78× | validated |
| 18 | 40.612 | — | — | timeout ([#585](https://github.com/redis/redis-adbc-driver/issues/585)) |
| 19 | 9.901 | — | — | timeout ([#586](https://github.com/redis/redis-adbc-driver/issues/586)) |
| 20 | 16.134 | 38437.549 | 2382.33× | validated |
| 21 | 22.246 | — | — | timeout ([#587](https://github.com/redis/redis-adbc-driver/issues/587)) |
| 22 | 12.479 | 12066.877 | 967.00× | validated |

Failure details:

- Query 1, redis: TIMEOUT: [redis] FT.CURSOR READ failed: context deadline exceeded. SQLSTATE: 57014
- Query 3, redis: TIMEOUT: [redis] FT.CURSOR READ failed: context deadline exceeded. SQLSTATE: 57014
- Query 4, redis: TIMEOUT: [redis] checking bounded scan index readiness failed: context deadline exceeded. SQLSTATE: 57014
- Query 5, redis: worker exited with code -9
- Query 7, redis: worker exited with code -9
- Query 8, redis: worker exited with code -9
- Query 9, redis: worker exited with code -9
- Query 18, redis: TIMEOUT: [redis] FT.CURSOR READ failed: context deadline exceeded. SQLSTATE: 57014
- Query 19, redis: TIMEOUT: [redis] FT.CURSOR READ failed: context deadline exceeded. SQLSTATE: 57014
- Query 21, redis: TIMEOUT: [redis] checking bounded scan index readiness failed: context deadline exceeded. SQLSTATE: 57014

## TPC-DS (Flex)

Deployment: Software Flex (experimental). Fully validated: 1/99.

| Query | DuckDB median (ms) | Redis median (ms) | Redis / DuckDB | Status |
|---|---:|---:|---:|---|
| 1 | 13.726 | 9805.355 | 714.39× | validated |
| 2 | 14.806 | — | — | timeout ([#588](https://github.com/redis/redis-adbc-driver/issues/588)) |
| 3 | 10.182 | — | — | timeout ([#589](https://github.com/redis/redis-adbc-driver/issues/589)) |
| 4 | 76.211 | — | — | timeout ([#590](https://github.com/redis/redis-adbc-driver/issues/590)) |
| 5 | 11.707 | — | — | timeout ([#591](https://github.com/redis/redis-adbc-driver/issues/591)) |
| 6 | 10.962 | — | — | timeout ([#592](https://github.com/redis/redis-adbc-driver/issues/592)) |
| 7 | 21.584 | — | — | timeout ([#593](https://github.com/redis/redis-adbc-driver/issues/593)) |
| 8 | 16.723 | — | — | process failure ([#594](https://github.com/redis/redis-adbc-driver/issues/594)) |
| 9 | 33.392 | — | — | timeout ([#595](https://github.com/redis/redis-adbc-driver/issues/595)) |
| 10 | 10.377 | — | — | interrupted by user |
| 11 | — | — | — | not run |
| 12 | — | — | — | not run |
| 13 | — | — | — | not run |
| 14 | — | — | — | not run |
| 15 | — | — | — | not run |
| 16 | — | — | — | not run |
| 17 | — | — | — | not run |
| 18 | — | — | — | not run |
| 19 | — | — | — | not run |
| 20 | — | — | — | not run |
| 21 | — | — | — | not run |
| 22 | — | — | — | not run |
| 23 | — | — | — | not run |
| 24 | — | — | — | not run |
| 25 | — | — | — | not run |
| 26 | — | — | — | not run |
| 27 | — | — | — | not run |
| 28 | — | — | — | not run |
| 29 | — | — | — | not run |
| 30 | — | — | — | not run |
| 31 | — | — | — | not run |
| 32 | — | — | — | not run |
| 33 | — | — | — | not run |
| 34 | — | — | — | not run |
| 35 | — | — | — | not run |
| 36 | — | — | — | not run |
| 37 | — | — | — | not run |
| 38 | — | — | — | not run |
| 39 | — | — | — | not run |
| 40 | — | — | — | not run |
| 41 | — | — | — | not run |
| 42 | — | — | — | not run |
| 43 | — | — | — | not run |
| 44 | — | — | — | not run |
| 45 | — | — | — | not run |
| 46 | — | — | — | not run |
| 47 | — | — | — | not run |
| 48 | — | — | — | not run |
| 49 | — | — | — | not run |
| 50 | — | — | — | not run |
| 51 | — | — | — | not run |
| 52 | — | — | — | not run |
| 53 | — | — | — | not run |
| 54 | — | — | — | not run |
| 55 | — | — | — | not run |
| 56 | — | — | — | not run |
| 57 | — | — | — | not run |
| 58 | — | — | — | not run |
| 59 | — | — | — | not run |
| 60 | — | — | — | not run |
| 61 | — | — | — | not run |
| 62 | — | — | — | not run |
| 63 | — | — | — | not run |
| 64 | — | — | — | not run |
| 65 | — | — | — | not run |
| 66 | — | — | — | not run |
| 67 | — | — | — | not run |
| 68 | — | — | — | not run |
| 69 | — | — | — | not run |
| 70 | — | — | — | not run |
| 71 | — | — | — | not run |
| 72 | — | — | — | not run |
| 73 | — | — | — | not run |
| 74 | — | — | — | not run |
| 75 | — | — | — | not run |
| 76 | — | — | — | not run |
| 77 | — | — | — | not run |
| 78 | — | — | — | not run |
| 79 | — | — | — | not run |
| 80 | — | — | — | not run |
| 81 | — | — | — | not run |
| 82 | — | — | — | not run |
| 83 | — | — | — | not run |
| 84 | — | — | — | not run |
| 85 | — | — | — | not run |
| 86 | — | — | — | not run |
| 87 | — | — | — | not run |
| 88 | — | — | — | not run |
| 89 | — | — | — | not run |
| 90 | — | — | — | not run |
| 91 | — | — | — | not run |
| 92 | — | — | — | not run |
| 93 | — | — | — | not run |
| 94 | — | — | — | not run |
| 95 | — | — | — | not run |
| 96 | — | — | — | not run |
| 97 | — | — | — | not run |
| 98 | — | — | — | not run |
| 99 | — | — | — | not run |

Failure details:

- Query 2, redis: TIMEOUT: [redis] failed to read rows: context deadline exceeded. SQLSTATE: 57014
- Query 3, redis: TIMEOUT: [redis] failed to read rows: context deadline exceeded. SQLSTATE: 57014
- Query 4, redis: TIMEOUT: [redis] failed to read rows: context deadline exceeded. SQLSTATE: 57014
- Query 5, redis: TIMEOUT: [redis] failed to read rows: context deadline exceeded. SQLSTATE: 57014
- Query 6, redis: TIMEOUT: [redis] failed to read rows: context deadline exceeded. SQLSTATE: 57014
- Query 7, redis: TIMEOUT: [redis] failed to read rows: context deadline exceeded. SQLSTATE: 57014
- Query 8, redis: worker exited with code -9
- Query 9, redis: TIMEOUT: [redis] failed to read rows: context deadline exceeded. SQLSTATE: 57014

## Separate focused follow-ups

The [TPC-H Q19 follow-up](benchmarks/olap-release/Q19-FOLLOWUP.md) records direct
Redis ADBC and DuckDB-over-Redis-ADBC paths. Those later observations do not
replace the released-driver baseline above; no full TPC-H rerun was completed.

The [TPC-DS focused follow-up](docs/performance/tpcds-focused-followup-2026-11-23.md)
records seven of eight originally failed targeted cases validated across multiple
combined development artifacts. Q4 remains unresolved. This is neither a
single-build pass rate nor a full 99-query result; full suites remain stopped.
