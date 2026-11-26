# Validation

2026-11-26 (UTC). Driver: main `7c83c486`. Harness: `benchmarks/olap-release` at the same commit, unchanged. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.** On Software Flex at SF1, on one build of the newest main:

| Suite | Validated | Timed out | Client OOM kill | Median of validated medians | Slowest validated median |
|---|---:|---:|---:|---:|---|
| SSB | **13 / 13** | 0 | 0 | 6.75 s | Q3.1 27.17 s |
| TPC-H | **14 / 22** | 8 | 0 | 10.19 s | Q5 40.47 s |
| TPC-DS | **79 / 99** | 15 | 5 | 2.68 s | Q67 35.56 s |

- Every validated query had one warm-up and three fully fetched timed runs under the 60 s statement deadline, each validated against native DuckDB. None of these 106 queries returned 0 rows.
- **Every failure happened in the warm-up,** so no failing query has a timed run. No run was repeated or discarded within a suite.
- **TPC-DS SF1 ran in full for the first time.** The five OOM kills were the Redis worker at the 8 GiB client limit (`oom_kills_delta` 1 each), 16–56 s into the warm-up.
- **TPC-DS Q75** hit the deadline, but the driver reported `UNKNOWN: [redis] context deadline exceeded`, not `TIMEOUT … SQLSTATE: 57014` as every other deadline in these runs did.
- **TPC-H Q15** timed out, although it validated in every [#648](https://github.com/redis/redis-adbc-driver/issues/648) pass. Two focused reruns also timed out, one of them with the older `064eb12d` library that validated it there ([below](#tpc-h-q15)).
- Per-query issues are linked from the PR.

## Deployment

- **Fixtures:** one per suite, each from `go/flex-setup.sh` with its own Compose project and ports 19743/19700, and each removed before the next started:

  | Suite | Fixture up | Loaded | Timed | Removed |
  |---|---|---|---|---|
  | SSB | 07:55:48Z | 07:56:16–08:02:52 | 08:02:53–08:18:54 | 08:19:12Z |
  | TPC-H | 08:19:17Z | 08:19:45–08:28:43 | 08:28:44–08:56:54 | 09:03:30Z (after the Q15 reruns) |
  | TPC-DS | 09:03:35Z | 09:04:03–09:17:33 | 09:17:33–10:34:24 | 10:35:10Z |

  - Override: `pull_policy: never`, 20 GiB container limit without swap. Image `redislabs/redis:latest` `679d117a56fc`, not re-pulled.
  - Database: Redis 8.8.0, bigstore v2, `search_on_bigstore`, 1 shard, 64 GiB with an 8 GiB RAM tier, `noeviction`, TLS. Set through the management API before loading: Search disk settings 8% / 2% / 3 MiB, AOF every second. `search-fork-gc-clean-threshold` 100, `search-fork-gc-run-interval` 30.
- **QPF:** the management API reported active 4x QPF (6 Search workers, `mnp`, 32 connections) with every setting above before loading, before timing and after timing of each suite. All nine checks passed.
- **Host:** Docker Desktop VM (16 CPUs, 31.5 GiB) on macOS, Apple silicon. During timing, only the suite's fixture and one client container ran. The TPC-DS server container used 17.3 GiB of its 20 GiB when its run ended.
- **Client:** `adbc-olap-release-client:20261123`, 4 CPUs and 8 GiB without swap, `--network container:redis-adbc-flex` (Linux namespace route on a macOS host). DuckDB 2.0.1, PyArrow 26.0.1, ADBC driver manager 1.13.0, `redis-py` 8.1.0, 4 client threads, shared client cgroup.
- **Driver:** `go/build-linux.sh arm64 v0.0.32-main.7c83c486` in `redis-adbc-linux-builder:1.27.4-arm64`, SHA-256 `717b20d98815519e1039b4a19f5cf4d0b6e6cceca8dd5f2dda1817f2e02be036`, loaded through the v0.0.31 wheel with `REDIS_ADBC_LIBRARY`. Driver options: `adbc.redis.flex=true` and the suite's `adbc.redis.default_schema`; worker options unset (Flex default 32). Worker grants are not in the harness records.
- **Data:** see the [spec](spec.md#what-is-measured). SHA-256s are in [`sources.sha256`](../../../../benchmarks/olap-release/results/sf1-flex-7c83c486/sources.sha256) and each suite's `queries.sha256`. Every table's count matched its source after loading and again before timing.

## Compaction gate

| Suite | Wait | Exempt (uncompacted, at most 10,000 docs) | Below the GC threshold |
|---|---|---|---|
| SSB | 225 s, 16 polls | date_ (2,556), supplier (2,000) | — |
| TPC-H | 225 s, 16 polls | supplier (10,000); compacted before Q15 ran | nation, region |
| TPC-DS | first poll | household_demographics (7,200), promotion (300) | call_center, income_band, reason, ship_mode, store, warehouse, web_page, web_site |

Every other index had completed a first disk GC cycle before its suite was timed, and the gate passed again before every query.

## Results

Median of the three timed runs (warm-up; runs). The earlier records come from other fixtures and cache states, so they give orientation, not a controlled comparison. Every query's record is in [`results/sf1-flex-7c83c486`](../../../../benchmarks/olap-release/results/sf1-flex-7c83c486/README.md).

### SSB

| Query | Rows | This run: median (warm-up; runs) | Earlier record (flex-ssb-compacted, main `ac82cfa0`) | DuckDB median |
|---|---:|---|---|---:|
| Q1.1 | 1 | 1.79 s (3.98 s; 2.11 s/1.79 s/1.78 s) | 1.9 s | 8 ms |
| Q1.2 | 1 | 491 ms (608 ms; 491 ms/493 ms/488 ms) | 0.59 s | 5 ms |
| Q1.3 | 1 | 140 ms (157 ms; 140 ms/140 ms/140 ms) | 0.15 s | 4 ms |
| Q2.1 | 280 | 25.16 s (33.92 s; 24.83 s/29.65 s/25.16 s) | 25.0 s | 8 ms |
| Q2.2 | 56 | 21.24 s (21.63 s; 21.42 s/21.07 s/21.24 s) | 21.4 s | 8 ms |
| Q2.3 | 7 | 5.07 s (5.12 s; 5.07 s/5.11 s/5.05 s) | 5.1 s | 6 ms |
| Q3.1 | 150 | 27.17 s (28.37 s; 26.92 s/27.23 s/27.17 s) | 27.6 s | 11 ms |
| Q3.2 | 596 | 13.68 s (13.81 s; 13.53 s/13.68 s/13.88 s) | 14.1 s | 7 ms |
| Q3.3 | 24 | 10.14 s (10.17 s; 10.05 s/10.20 s/10.14 s) | 10.3 s | 6 ms |
| Q3.4 | 3 | 6.75 s (7.13 s; 6.75 s/6.90 s/6.75 s) | 6.9 s | 5 ms |
| Q4.1 | 35 | 27.04 s (29.64 s; 27.37 s/27.04 s/26.78 s) | 28.8 s | 11 ms |
| Q4.2 | 100 | 6.51 s (7.64 s; 6.45 s/6.51 s/6.60 s) | 6.9 s | 12 ms |
| Q4.3 | 330 | 2.06 s (2.11 s; 2.09 s/2.05 s/2.06 s) | 2.1 s | 7 ms |

### TPC-H

| Query | Rows | This run: median (warm-up; runs) | Earlier record (#648, PR #689 build) | DuckDB median |
|---|---:|---|---|---:|
| Q1 | 4 | timeout in warm-up | timeout | 16 ms |
| Q2 | 100 | 13.49 s (13.91 s; 13.53 s/13.49 s/13.46 s) | 14.9 s | 4 ms |
| Q3 | 10 | timeout in warm-up | timeout | 10 ms |
| Q4 | 5 | timeout in warm-up | timeout | 14 ms |
| Q5 | 5 | 40.47 s (50.09 s; 40.47 s/40.91 s/40.19 s) | 48.1 s | 10 ms |
| Q6 | 1 | 4.30 s (5.96 s; 4.39 s/4.30 s/4.23 s) | 4.5 s | 4 ms |
| Q7 | 4 | 14.45 s (19.27 s; 14.45 s/14.33 s/14.77 s) | 24.1 s | 13 ms |
| Q8 | 2 | 19.54 s (21.48 s; 19.53 s/20.30 s/19.54 s) | 22.2 s | 11 ms |
| Q9 | 175 | timeout in warm-up | timeout | 30 ms |
| Q10 | 20 | timeout in warm-up | timeout | 24 ms |
| Q11 | 1,048 | 814 ms (1.90 s; 779 ms/828 ms/814 ms) | 0.9 s | 5 ms |
| Q12 | 2 | 9.39 s (17.49 s; 11.28 s/9.39 s/8.88 s) | 14.2 s | 9 ms |
| Q13 | 42 | 12.87 s (11.07 s; 13.07 s/12.87 s/12.48 s) | 16.4 s | 48 ms |
| Q14 | 1 | 1.83 s (4.58 s; 1.93 s/1.83 s/1.81 s) | 1.9 s | 10 ms |
| Q15 | 1 | timeout in warm-up | 23.3 s | 6 ms |
| Q16 | 18,314 | 5.13 s (8.90 s; 5.11 s/5.22 s/5.13 s) | 7.1 s | 15 ms |
| Q17 | 1 | 10.99 s (11.64 s; 11.00 s/10.99 s/10.96 s) | 12.9 s | 10 ms |
| Q18 | 57 | 27.14 s (54.14 s; 26.93 s/27.14 s/27.96 s) | 49.1 s | 32 ms |
| Q19 | 1 | 2.53 s (4.14 s; 2.54 s/2.53 s/2.41 s) | 3.0 s | 9 ms |
| Q20 | 186 | timeout in warm-up | timeout | 12 ms |
| Q21 | 100 | timeout in warm-up | timeout | 24 ms |
| Q22 | 7 | 9.34 s (13.22 s; 9.38 s/9.06 s/9.34 s) | 11.9 s | 12 ms |

- Q1, Q3, Q4, Q9, Q10 and Q21 read most of `lineitem` or `orders` from flash on one shard, and Q20 runs one Search call per `partsupp` row. They are already tracked in [#636](https://github.com/redis/redis-adbc-driver/issues/636) and [#658](https://github.com/redis/redis-adbc-driver/issues/658).
- Q2, Q7 and Q8 keep the PR #689 improvements (13.5 s, 14.4 s and 19.5 s).

### TPC-H Q15

Q15 validated in all three #648 passes, with medians of 20.5–23.3 s but warm-ups of 33.5–47.7 s. Here it timed out in the warm-up, while reading the row directory, after Q14 had run. Before removing the TPC-H fixture, two focused runs (`run.py --load-only --reuse-loaded`, then `--query 15`, into new output directories) followed the suite:

| Run | Library | Result |
|---|---|---|
| Suite pass | main `7c83c486` | timeout in warm-up (`failed to read snapshot row directory`) |
| Focused rerun | main `7c83c486` | timeout in warm-up (`failed to read rows`), 60.4 s |
| Focused rerun | main `064eb12d` (SHA-256 `d1943a44…`), which validated Q15 in #648 | timeout in warm-up (`failed to read rows`), 60.4 s |

The older library times out the same way on this fixture, so the changes merged since #648 do not explain the timeout. What differs from the #648 fixture was not established: its indexes were not gated for compaction, and its RAM tier held other data.

### TPC-DS

Of the 79 validated queries: 20 under 1 s, 38 from 1 to 10 s, 18 from 10 to 30 s and 3 above 30 s. Q51 and Q76 validated but their warm-ups took 58.3 and 58.9 s. The ten slowest:

| Query | Rows | Redis median (warm-up; runs) |
|---|---:|---|
| Q67 | 100 | 35.56 s (36.23 s; 36.67 s/35.56 s/29.95 s) |
| Q51 | 100 | 34.69 s (58.30 s; 35.15 s/34.69 s/33.81 s) |
| Q76 | 100 | 32.97 s (58.91 s; 34.06 s/32.97 s/32.90 s) |
| Q70 | 3 | 28.52 s (31.14 s; 29.14 s/28.52 s/28.28 s) |
| Q65 | 100 | 24.42 s (26.87 s; 24.42 s/23.63 s/24.85 s) |
| Q9 | 1 | 22.65 s (24.21 s; 22.65 s/22.61 s/23.39 s) |
| Q38 | 1 | 21.55 s (26.75 s; 27.69 s/21.55 s/20.20 s) |
| Q87 | 1 | 21.26 s (26.89 s; 21.80 s/21.26 s/20.77 s) |
| Q48 | 1 | 21.23 s (33.44 s; 21.47 s/21.23 s/20.55 s) |
| Q11 | 88 | 17.95 s (21.21 s; 19.97 s/17.95 s/17.06 s) |

The 20 failures, all in the warm-up:

| Failure | Queries | Reported error |
|---|---|---|
| Deadline | Q4, Q14, Q18, Q22, Q35, Q39, Q64 | `TIMEOUT: the query timed out` |
| Deadline | Q2, Q28, Q40, Q85 | `TIMEOUT: failed to read rows` |
| Deadline | Q23, Q49 | `TIMEOUT: failed to seek snapshot partition directory` |
| Deadline | Q44 | `TIMEOUT: failed to read snapshot row directory` |
| Deadline | Q75 | `UNKNOWN: context deadline exceeded`, without SQLSTATE 57014 |
| Client OOM kill (8 GiB) | Q72 (55.7 s), Q78 (43.5 s), Q80 (27.4 s), Q93 (35.3 s), Q95 (16.2 s) | worker exited with code -9 |

Of the first ten queries, the only ones the v0.0.31 Flex run reached, Q3 and Q5–Q10 now validate (Q8 was OOM-killed then). Q2 and Q4 still time out on main. Their v0.0.31 issues, [#588](https://github.com/redis/redis-adbc-driver/issues/588) and [#590](https://github.com/redis/redis-adbc-driver/issues/590), were closed after validation on combined candidate builds, not on main. All 99 queries validated at [SF 0.1 on Flex](../sf01-flex-suites/validation.md).

## Commands

Per suite, in the client container described above, with that suite's private profile at `/private`:

```sh
python run.py <suite> --private /private --output /work/results/load --load-only
python supervise.py <suite> --private /private --output /work/results/run --reuse-loaded
```

As in the [SF 0.1 record](../sf01-flex-suites/validation.md#commands), the first command was stopped once its tables had loaded and been count-checked, so that the profile could get its `flex-index-created.json`. `supervise.py` exited 0 for all three suites.

## Not established

- **Causes:** no execution diagnostics, worker grants, rows read or RAM/flash object split were recorded for the failing queries. Each issue lists what is needed.
- **Cache state:** each suite ran once, in order, on a freshly loaded fixture. Timings depend on what earlier queries left in the 8 GiB RAM tier.
- **Variance:** no repeat pass or second fixture.
- **DuckDB over Redis ADBC:** not run.
- **Other deployments:** more shards, a larger RAM tier, Redis Cloud, other Software or Search versions.
- **Review:** human review is pending.

## Fixture cost and ownership

Owned by this session: local Docker on the developer's Mac, trial license, no cloud resources, no cost. Each fixture and its Compose network were removed after its suite (times above); the service has no volumes. Private profiles, sources and logs stay in the git-ignored `go/build/sf1-flex/`.
