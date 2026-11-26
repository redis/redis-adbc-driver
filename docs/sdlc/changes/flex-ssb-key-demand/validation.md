# Validation

2026-11-25/26 (UTC), branch from main `844d8123` (includes #689). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **SSB SF1 on Flex:** this change validated 4 of 13 queries (Q1.1, Q1.2, Q1.3, Q4.3), against 2 on main in the same interleaved run (Q1.2, Q1.3).
- **Faster:** Q1.2 went from 17.8 s to 0.9 s, and Q1.3 from 4.0 s to 0.7 s. Q1.1 and Q4.3 time out on main.
- **Still timing out:** Q2.1–Q3.4, Q4.1 and Q4.2. Each still pushes at least 278 key terms. On this deployment every term of a `lineorder` key union, point or range, costs about 0.55 s per Search evaluation, whatever it matches, so their single evaluation exceeds the 60 s deadline.
- **Index layout:** in the same database, the same 100-term union costs about 45× less on TPC-H `lineitem`. `FT.INFO` shows a different flash layout for `lineorder`; see [below](#why-the-other-queries-time-out).

## Deployment

The server is the one #689 used, with TPC-H SF1 and SSB SF1 already loaded; see [its record](../flex-search-union-cost/validation.md#deployment).
- **Server:** Redis Software 8.8.0 Flex, 1 shard, 64 GiB database with an 8 GiB RAM tier, `noeviction`, TLS, AOF every second, 20 GiB container cap.
- **QPF:** active 4x QPF (6 Search workers, `mnp`, 32 connections) was verified through the management API by the session that provisioned and owned the server. This session did not query the API itself before the server was removed after these runs.
- **Data:** SSB SF1 in schema `ssb`, loaded with main `064eb12d`. Not reloaded: before its queries, each build count-validated all five tables (`--reuse-loaded`).
- **Client:** one container at a time (`adbc-olap-release-client:20261123`), 4 CPUs and 8 GiB without swap, through `--network container:redis-adbc-flex` (Linux namespace route, macOS host). The installed v0.0.31 wheel was pointed at each library with `REDIS_ADBC_LIBRARY`.
- **Builds:** Linux ARM64 libraries built with `go/build-linux.sh` (c-shared, `-tags driverlib`, version linker flag) in `redis-adbc-linux-builder:1.27.4-arm64`. SHA-256 prefixes:
  - main `844d8123`: `686f8dfe`;
  - this change: `f7c91495`. It was built from the working tree before the final commit, which differs only in the comment above `integerRunUnionQuery`.

## SSB SF1 on Flex

`benchmarks/olap-release/run.py ssb --reuse-loaded --query N`, direct Redis only, 2026-11-26 00:37–01:03Z. For each query, each build ran in a fresh client container, and the order alternated from query to query (main first for Q1.1). Each run is a warm-up and three timed runs under a 60 s statement deadline, fully fetched and validated against DuckDB. All 13 native DuckDB references succeeded.

Direct Redis ADBC, median of the timed runs (warm-up; the three runs). Terms are the key-union terms pushed into `lineorder`'s filter, from the demand analysis below.

| Query | main `844d8123` | terms | **this change** | terms | #689 run (`064eb12d`) |
|---|---|---:|---|---:|---|
| Q1.1 | timeout | 365 | **9.1 s** (10.9; 9.1/8.9/9.3) | 12 | timeout |
| Q1.2 | 17.8 s (18.0; 18.1/17.8/17.6) | 31 | **0.9 s** (1.1; 0.9/0.9/0.9) | 1 | 19.6 s |
| Q1.3 | 4.0 s (4.1; 4.0/4.0/4.2) | 7 | **0.7 s** (0.7; 0.7/0.7/0.7) | 1 | 4.5 s |
| Q2.1 | timeout | 2,934 | timeout | 301 | timeout |
| Q2.2 | timeout | 4,589 | timeout | 1,924 | timeout |
| Q2.3 | timeout | 3,139 | timeout | 514 | timeout |
| Q3.1 | timeout | 2,641 | timeout | 422 | timeout |
| Q3.2 | timeout | 3,528 | timeout | 1,343 | timeout |
| Q3.3 | timeout | 2,470 | timeout | 349 | timeout |
| Q3.4 | timeout | 309 | timeout | 278 | timeout |
| Q4.1 | timeout | 2,934 | timeout | 301 | timeout |
| Q4.2 | timeout | 1,107 | timeout | 325 | timeout |
| Q4.3 | timeout | 805 | **57.6 s** (58.0; 57.6/58.6/56.3) | 95 | timeout |

- **Timeouts:** every timeout, on both builds, was the driver's own 60 s deadline in "failed complete snapshot candidate read" (SQLSTATE 57014), that is, the fact's single Search evaluation.
- **Validated:** 4 of 13, against 2 on main. RAM with v0.0.31 validated all 13 (`PERFORMANCE.md`).
- **Q4.3** passes with 1.4–3.7 s to spare in each run. Its 71 supplier terms alone take about 39 s (below).
- **Last column:** #689's own run is from [its record](../flex-search-union-cost/validation.md#ssb-sf1-658), another window, for reference only.

### What each query pushes

From the SSB SF1 DuckDB reference (read-only): each dimension's distinct keys, the runs of consecutive keys among them, and the `lineorder` rows they match. *F* is a filtered dimension, *U* an unfiltered one. Pushed means at most 4,096 keys for a filtered dimension. With this change an unfiltered dimension needs at most 1,000; on main its limit is also 4,096.

| Query | date | supplier | customer | part |
|---|---|---|---|---|
| Q1.1 | F 365 keys, 12 runs, 908,288 rows | — | — | — |
| Q1.2 | F 31, 1 run, 78,532 | — | — | — |
| Q1.3 | F 7, 1 run, 17,317 | — | — | — |
| Q2.1 | U 2,556, 84 runs (main only) | F 378, 301 runs, 1,134,371 | — | F 7,883 (not pushed) |
| Q2.2 | U 2,556 (main only) | F 449, 350 runs, 1,347,130 | — | F 1,584, 1,574 runs, 47,301 |
| Q2.3 | U 2,556 (main only) | F 380, 311 runs, 1,140,311 | — | F 203, 203 runs, 5,848 |
| Q3.1 | F 2,192, 72 runs, 5,466,178 | F 449, 350 runs | F 6,051 (not pushed) | — |
| Q3.2 | F 2,192, 72 runs | F 76, 71 runs, 228,595 | F 1,260, 1,200 runs, 246,734 | — |
| Q3.3 | F 2,192, 72 runs | F 19, 19 runs, 56,901 | F 259, 258 runs, 50,123 | — |
| Q3.4 | F 31, 1 run, 77,451 | F 19, 19 runs | F 259, 258 runs | — |
| Q4.1 | U 2,556 (main only) | F 378, 301 runs | F 5,992 (not pushed) | F 80,045 (not pushed) |
| Q4.2 | F 729, 24 runs, 1,444,984 | F 378, 301 runs | F 5,992 (not pushed) | F 80,045 (not pushed) |
| Q4.3 | F 729, 24 runs | F 76, 71 runs | F 5,992 (not pushed) | F 8,029 (not pushed) |

- **Dates:** a month is one run, so date demands shrink 12–31×.
- **Other keys:** supplier, customer and part keys in a region or brand are scattered, so they coalesce little (378 suppliers into 301 runs).
- **Unfiltered dates:** Q2.x and Q4.1 no longer push their 2,556 dates.

### Why the other queries time out

A read-only probe timed the coalesced unions on the server's `lineorder` index, twice each (`FT.SEARCH … NOCONTENT LIMIT 0 0 TIMEOUT 300000 DIALECT 2` through `redis-cli` inside the server container), after the runs above.

| Union | Terms | Matches | Seconds |
|---|---:|---:|---|
| Q3.4's customers | 258 | 50,123 | 143.6, 143.9 |
| Q3.4's suppliers | 19 | 56,901 | 10.8, 10.4 |
| Q3.4's date range | 1 | 77,451 | 1.0, 0.7 |
| Q3.4's three, intersected | 278 | 7 | 145.0, 144.1 |
| Q4.3's suppliers | 71 | 228,595 | 39.0, 38.7 |
| Q4.3's date ranges | 24 | 1,444,984 | 15.1, 14.7 |
| Q4.3's suppliers and date ranges | 95 | 54,887 | 51.0, 50.7 |
| Q2.3's parts | 203 | 5,848 | 113.8, 114.4 |

- **Per term:** a term costs about 0.55–0.62 s, point or range. It barely depends on what the term matches: 29 rows for a part, 194 for a customer, about 3,000 for a supplier, about 60,000 for a monthly range.
- **Additive:** an intersection costs about the sum of its unions, so a demand of n terms costs about 0.55·n s. That puts every query of 278 or more terms above 150 s.
- **Correction:** the "8 µs against 240 µs per matching row" in #689's SSB note follows from this per-term cost. A range is not cheaper per row; ranges help because a month of dates is one term instead of 28–31.

**Same database, other index.** 100 point terms with about 3,000 matches cost far more on SSB `lineorder` than on TPC-H `lineitem`, which has nearly the same number of documents:

| Index (documents) | Union | Matches | Seconds |
|---|---|---:|---|
| TPC-H `lineitem` (6,001,215) | 100 `l_partkey` | 2,941 | 2.6, 1.3 |
| TPC-H `lineitem` | 100 `l_suppkey` | 60,163 | 1.8, 1.2 |
| SSB `lineorder` (6,001,171) | 100 `lo_partkey` | 2,992 | 56.3, 57.0 |

`FT.INFO` shows a different flash layout for the slow indexes:
- **`lineorder`:** 18.0M inverted-index blocks, against 4.7M for `lineitem`. Each field has one disk key per document (6,001,171), against about 272,000 for `lineitem`.
- **TPC-H `orders`:** the same per-document layout (1,500,000 disk keys per field, 6.0M blocks). #689 measured 137 ms per `o_custkey` term on it.
- **Cost per indexed document:** 137 ms over 1.5M documents and 0.56 s over 6.0M are both about 91–93 ns per document per term.

Why these indexes have this layout is not established. The two loads used builds that differ only in commits that do not touch indexing (#683–#685). With `lineitem`'s per-term cost (12–26 ms here), the 278–1,924 terms the timed-out queries push would take about 3–50 s.

## Tests

- **Unit (fail without the change):**
  - `TestFlexFactKeyDemandCoalescesIntegerRuns` fails with `query.go` reverted, and shows one term per key.
  - `TestFlexUnfilteredDimensionDemandKeepsUnionLimit` fails with the cap reverted: the unfiltered demand is built.
  - `TestStarDimensionUnfilteredProof` covers the proof cases.
  - `TestFlexFactKeyDemandUnionLimit` now uses odd keys, and still requires all 1,500 terms.
- **Integration:** `TestSQLFlexStarKeyDemandRangesAndUnfilteredDimensions` (Redis, Flex option, 4 workers) passes. It fails with `query.go` reverted (365 point terms), with the cap reverted (supplier demand pushed), and with only the snowflake planner's `unfiltered` assignment removed (branch supplier demand pushed).
- **Unit suite:** `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...` passes (all 7 packages).
- **Fixture:** a dedicated Redis Software 8.8.0 database brought up with `software-setup.sh`: 3 shards, setup-verified effective 4x QPF (6 Search workers, `mnp`, 32 connections). It ran in its own Compose project (`adbc-ssb-demand`, ports 19643/16579), with the local image not re-pulled. It was stopped during the SSB runs.
- **Targeted integration:** `make -C go test-software TEST_ARGS="-run 'Flex|Snowflake|Star|Candidate|Demand|Join|Q19|SemiJoin|Union|InList|Subquery|Correlat|Partition|Aggregate' -skip 'LiveHash|Retained|Snapshot'"` gave 269 passed, 39 skipped, 0 failed.
  - 16 skips need a dedicated snapshot database, and 5 a disposable Flex database (`REDIS_FLEX_URI`).
  - The others need caller-owned restart/failover targets, a profiling row count, or protocol ACL commands Software blocks.
- **Snapshot suites:** on the same disposable fixture as `REDIS_SNAPSHOT_URI`, with `-race`, `-run 'Snapshot.*(Flex|Candidate|Partition|Aggregate|Star|Join|Directory|Tuple)|TestSnapshotPartitioned'` gave 52 passed, 6 skipped, 0 failed, and no data race. The skips need a snapshot cluster, profiling opt-ins or the API lease-model gate.
- `gofmt` clean for the changed files. `go vet` reports only the existing copied-lock warning.

## Not established

- **Index layout:** why `lineorder` and `orders` have one disk key per document, and whether rebuilding their indexes changes the per-term cost.
- **TPC-H on Flex:** not rerun. Ranges only reduce a demand's terms. No TPC-H SF1 dimension has an unfiltered key set of 1,001–4,096 keys (nation and region are smaller, the other tables larger), so the cap changes no TPC-H SF1 plan.
- **Selective unfiltered dimensions:** a small dimension table can be selective without a filter of its own. Above 1,000 keys its Flex demand is no longer pushed, as before #644.
- **Cold cache:** no controlled series. Q4.3 sits 1.4–3.7 s under the deadline.
- **Other deployments:** a larger RAM tier, more shards, and Redis Cloud.
- **QPF:** this session did not re-verify it on the Flex server itself (see Deployment).
- **Review:** human review is pending.
