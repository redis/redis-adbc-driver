# Validation

2026-11-26 (UTC). Driver: main `a15b1e5c`. Harness: `benchmarks/olap-release` at the same commit, unchanged. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.** On Software Flex at SF 0.1, every query of the three suites validated on one build:

| Suite | Validated | Median of query medians | 90th percentile | Slowest median | Client OOM kills |
|---|---:|---:|---:|---|---:|
| TPC-H | **22 / 22** | 0.77 s | 2.24 s | Q20 7.14 s | 0 |
| SSB | **13 / 13** | 0.19 s | 0.36 s | Q4.3 1.54 s | 0 |
| TPC-DS | **99 / 99** | 0.45 s | 2.43 s | Q78 5.46 s | 0 |

- Each query had one warm-up and three timed runs under the 60 s statement deadline, every result fully fetched and validated against native DuckDB. The slowest single run in any suite was 7.15 s.
- Nothing timed out, was killed or failed validation. No run was repeated or discarded.
- **Empty answers.** 16 queries return 0 rows at this scale, from both engines: SSB Q3.3 and Q3.4, and TPC-DS Q8, Q24, Q25, Q37, Q41, Q44, Q54, Q58, Q64, Q65, Q82, Q85, Q91 and Q93. They validate, but they do not check join or aggregate output.
- **Not comparable** with the SF1 Flex records (other data size and cache state) or with the [RAM SF 0.1 TPC-DS check](../tpcds-joined-semijoin/validation.md) (3 RAM shards, a macOS client on a published port, one cold run per query).

## Deployment

- **Server:** a dedicated fixture from `go/flex-setup.sh`, started 05:25:43Z.
  - Compose project `adbc-sf01-flex`, ports 19743/19700. Override: `pull_policy: never`, 20 GiB container limit without swap.
  - Image `redislabs/redis:latest` `679d117a56fc`, not re-pulled; the image of the [compaction record](../flex-index-compaction/validation.md#deployment).
  - Database: Redis 8.8.0, bigstore v2, `search_on_bigstore`, 1 shard, 64 GiB with an 8 GiB RAM tier, `noeviction`, TLS.
  - Set through the management API before loading: Search disk settings 8% / 2% / 3 MiB, AOF every second. `search-fork-gc-clean-threshold` 100 and `search-fork-gc-run-interval` 30 (defaults).
- **QPF:** the management API reported active 4x QPF (6 Search workers, `mnp`, 32 connections) with every setting above at 05:26:30Z, and before and after each suite: 05:30:03, 05:41:11, 05:41:36 and 05:48:22Z. All seven checks passed.
- **Host:** Docker Desktop VM (16 CPUs, 31.5 GiB) on macOS, Apple silicon. During timing, only the fixture and one client container ran.
- **Client:** `adbc-olap-release-client:20261123`, 4 CPUs and 8 GiB without swap, `--network container:redis-adbc-flex` (Linux namespace route on a macOS host). DuckDB 2.0.1, PyArrow 26.0.1, ADBC driver manager 1.13.0, `redis-py` 8.1.0, 4 client threads, shared client cgroup (no `worker_memory_limit_bytes`).
- **Driver:** `go/build-linux.sh arm64 v0.0.32-main.a15b1e5c` in `redis-adbc-linux-builder:1.27.4-arm64`. SHA-256 `730e4e7839f72291e35207c73b9a5e045d194bc14793a16ef16e6b464df4d362`. The installed v0.0.31 wheel loaded it through `REDIS_ADBC_LIBRARY`. Driver options: `adbc.redis.flex=true` and the suite's `adbc.redis.default_schema`; worker options unset, so the Flex default of 32 applies. Actual worker grants are not in the harness records.
- **Data:** generated as in the [spec](spec.md#what-is-measured). Source SHA-256s are in [`sources.sha256`](../../../../benchmarks/olap-release/results/sf01-flex-a15b1e5c/sources.sha256) and each suite's exported SQL hashes in its `queries.sha256`.
- **Layout:** one database, three schemas (`tpch`, `tpcds`, `ssb`). All three suites loaded in parallel at 05:27–05:30Z, before any timing, and every table's count matched its source. The suites were timed one at a time: TPC-H 05:30:03–05:41:11, SSB 05:41:11–05:41:36, TPC-DS 05:41:36–05:48:22Z. SF 0.1 of all three (2.7M rows) is well inside the 8 GiB RAM tier; flash reads were not measured.

## Compaction gate

Each suite's setup mapped every Search index to exactly one of its own tables; the other suites' indexes were counted as unmatched. Before every query, the gate was rechecked and passed.

| Suite | Wait | Compacted | Exempt (uncompacted, at most 10,000 docs) | Below the GC threshold (100 docs) |
|---|---|---|---|---|
| TPC-H | 511 s, 35 polls | customer, lineitem, orders, part, partsupp | supplier (1,000) | nation, region |
| SSB | first poll | lineorder, part | customer (3,000), date_ (2,556), supplier (200) | — |
| TPC-DS | first poll | catalog_page, catalog_returns, catalog_sales, customer, customer_demographics, date_dim, inventory, store_returns, store_sales, time_dim, web_sales | customer_address (5,000), household_demographics (7,200), item (1,800), web_returns (7,061) | call_center, income_band, promotion, reason, ship_mode, store, warehouse, web_page, web_site |

The exemption's `FT.CREATE` times come from the shard log's "Successfully created index" lines, matched to tables by the gate's own field rule.

### Small indexes without a first cycle

This adds to the [earlier observation](../flex-ssb-compacted/validation.md#small-indexes-without-a-first-cycle):
- **Not compacted:** eight indexes of 200–7,200 documents had `total_cycles` 0 at the last per-query check, 11.9–19.8 minutes after `FT.CREATE`. Three of them, `customer_address`, `household_demographics` and `web_returns`, were loaded in two 4,096-row Arrow batches, not one.
- **Compacted:** every index of 10,000 or more documents, including TPC-DS `customer` (10,000 documents, three batches, loaded in 0.52 s).
- **Cost while uncompacted:** one point term on the exempt indexes took 0.07–0.72 ms in total (`DISK_NUMERIC`).
- **Not explained.** Single-batch loading is therefore not a sufficient description. The cause is still not established.

## Results

Median of the three timed runs (warm-up; runs). DuckDB medians are native DuckDB on the same SF 0.1 source. The query records are in [`results/sf01-flex-a15b1e5c`](../../../../benchmarks/olap-release/results/sf01-flex-a15b1e5c/README.md).

### TPC-H

| Query | Rows | Redis median (warm-up; runs) | DuckDB median |
|---|---:|---|---:|
| Q1 | 4 | 2.04 s (2.08 s; 2.04 s/2.06 s/2.01 s) | 3 ms |
| Q2 | 44 | 695 ms (696 ms; 736 ms/695 ms/689 ms) | 3 ms |
| Q3 | 10 | 2.24 s (2.25 s; 2.24 s/2.29 s/2.22 s) | 6 ms |
| Q4 | 5 | 3.16 s (3.17 s; 3.06 s/3.23 s/3.16 s) | 2 ms |
| Q5 | 5 | 922 ms (975 ms; 926 ms/922 ms/919 ms) | 4 ms |
| Q6 | 1 | 484 ms (511 ms; 484 ms/482 ms/484 ms) | 1 ms |
| Q7 | 4 | 526 ms (544 ms; 531 ms/523 ms/526 ms) | 3 ms |
| Q8 | 2 | 758 ms (776 ms; 757 ms/758 ms/758 ms) | 3 ms |
| Q9 | 175 | 1.57 s (1.71 s; 1.60 s/1.55 s/1.57 s) | 7 ms |
| Q10 | 20 | 966 ms (970 ms; 946 ms/966 ms/971 ms) | 4 ms |
| Q11 | 2,541 | 101 ms (113 ms; 101 ms/102 ms/101 ms) | 3 ms |
| Q12 | 2 | 671 ms (718 ms; 672 ms/671 ms/658 ms) | 2 ms |
| Q13 | 37 | 774 ms (769 ms; 808 ms/771 ms/774 ms) | 11 ms |
| Q14 | 1 | 222 ms (242 ms; 224 ms/222 ms/222 ms) | 2 ms |
| Q15 | 1 | 1.60 s (1.65 s; 1.67 s/1.60 s/1.60 s) | 2 ms |
| Q16 | 2,762 | 592 ms (575 ms; 575 ms/612 ms/592 ms) | 4 ms |
| Q17 | 1 | 115 ms (124 ms; 115 ms/116 ms/110 ms) | 3 ms |
| Q18 | 5 | 1.70 s (1.79 s; 1.70 s/1.70 s/1.70 s) | 6 ms |
| Q19 | 1 | 190 ms (214 ms; 190 ms/204 ms/190 ms) | 3 ms |
| Q20 | 9 | 7.14 s (7.15 s; 7.14 s/7.12 s/7.15 s) | 3 ms |
| Q21 | 47 | 6.04 s (5.96 s; 6.04 s/6.01 s/6.09 s) | 5 ms |
| Q22 | 7 | 770 ms (787 ms; 770 ms/764 ms/776 ms) | 4 ms |

### SSB

| Query | Rows | Redis median (warm-up; runs) | DuckDB median |
|---|---:|---|---:|
| Q1.1 | 1 | 190 ms (252 ms; 190 ms/193 ms/190 ms) | 2 ms |
| Q1.2 | 1 | 301 ms (319 ms; 302 ms/299 ms/301 ms) | 2 ms |
| Q1.3 | 1 | 108 ms (115 ms; 109 ms/108 ms/107 ms) | 2 ms |
| Q2.1 | 280 | 231 ms (269 ms; 230 ms/231 ms/241 ms) | 3 ms |
| Q2.2 | 56 | 193 ms (206 ms; 200 ms/192 ms/193 ms) | 2 ms |
| Q2.3 | 7 | 98 ms (102 ms; 99 ms/97 ms/98 ms) | 2 ms |
| Q3.1 | 150 | 427 ms (519 ms; 429 ms/427 ms/420 ms) | 3 ms |
| Q3.2 | 394 | 144 ms (145 ms; 144 ms/144 ms/141 ms) | 3 ms |
| Q3.3 | 0 | 28 ms (30 ms; 28 ms/28 ms/27 ms) | 1 ms |
| Q3.4 | 0 | 9 ms (13 ms; 9 ms/8 ms/9 ms) | 2 ms |
| Q4.1 | 35 | 365 ms (373 ms; 367 ms/365 ms/363 ms) | 3 ms |
| Q4.2 | 100 | 321 ms (316 ms; 328 ms/306 ms/321 ms) | 3 ms |
| Q4.3 | 48 | 1.54 s (1.66 s; 1.57 s/1.54 s/1.54 s) | 3 ms |

### TPC-DS

Distribution of the 99 medians: 20 below 0.1 s, 51 from 0.1 to 1 s, 22 from 1 to 3 s, 6 from 3 to 5.5 s. The ten slowest:

| Query | Rows | Redis median (warm-up; runs) |
|---|---:|---|
| Q78 | 100 | 5.46 s (5.61 s; 5.44 s/5.46 s/5.49 s) |
| Q14 | 100 | 4.38 s (4.26 s; 4.41 s/4.21 s/4.38 s) |
| Q23 | 1 | 3.83 s (3.90 s; 3.86 s/3.83 s/3.75 s) |
| Q61 | 1 | 3.71 s (3.74 s; 3.70 s/3.71 s/3.74 s) |
| Q76 | 100 | 3.22 s (3.22 s; 3.23 s/3.21 s/3.22 s) |
| Q80 | 100 | 3.13 s (3.14 s; 3.12 s/3.13 s/3.15 s) |
| Q4 | 1 | 2.87 s (2.86 s; 2.87 s/2.87 s/2.88 s) |
| Q75 | 23 | 2.67 s (2.71 s; 2.67 s/2.66 s/2.68 s) |
| Q64 | 0 | 2.53 s (2.57 s; 2.53 s/2.54 s/2.53 s) |
| Q83 | 6 | 2.45 s (2.50 s; 2.46 s/2.45 s/2.44 s) |

The full table is in the [results README](../../../../benchmarks/olap-release/results/sf01-flex-a15b1e5c/README.md#tpc-ds).

## Commands

Per suite, in the client container described above (`/private` is that suite's private profile):

```sh
python run.py <suite> --private /private --output /work/results/load --load-only
python supervise.py <suite> --private /private --output /work/results/run --reuse-loaded
```

The first command was stopped (`docker kill`) once its tables had loaded and been count-checked, because its gate reads `flex-index-created.json` when it starts and so cannot apply the exemption. The second recounted every table, passed the gate and ran every query; it exited 0 for all three suites.

Harness unit tests, with no harness file changed: `python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py'` ran 64 tests, all passing, 1 skipped (the opt-in cgroup OOM test). Link check of the new Markdown: no missing files or anchors.

## Not established

- **SF1:** none of this predicts SF1 behavior on Flex, where data exceeds the 8 GiB RAM tier ([#636](https://github.com/redis/redis-adbc-driver/issues/636), [#648](https://github.com/redis/redis-adbc-driver/issues/648)).
- **Variance:** one pass, no cold-cache series, no second fixture.
- **DuckDB over Redis ADBC:** not run.
- **Worker grants and client memory peaks:** not recorded by the harness in this configuration.
- **Other deployments:** more shards, a larger RAM tier, Redis Cloud, other Software or Search versions.
- **Review:** human review is pending.

## Fixture cost and ownership

Owned by this session: local Docker on the developer's Mac, trial license, no cloud resources, no cost. The container and its Compose network were removed at 05:49:03Z (`docker compose --profile flex down`); the service has no volumes. Private profiles, generated data and logs stay in the git-ignored `go/build/sf01-flex/`.
