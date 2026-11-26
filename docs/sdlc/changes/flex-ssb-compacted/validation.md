# Validation

2026-11-26 (UTC). Driver: main `ac82cfa0`. Harness: this change. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **SSB SF1 on Flex: 13 of 13 validated** on main `ac82cfa0`, with the opt-in small-index exemption. The [2026-11-26 key-demand run](../flex-ssb-key-demand/validation.md#ssb-sf1-on-flex) validated 4 of 13.
- **Compaction state:**
  - `customer`, `lineorder` and `part` had each completed a first disk GC cycle before timing.
  - `date_` (2,556 rows) and `supplier` (2,000) never got one. They were timed uncompacted under the exemption, and every query record carries their state.
- **The nine queries that timed out** now validate in 5.1–28.8 s.
- **Q2.2,** the largest demand (1,924 terms), takes 21.4 s, close to the 22 s estimated from the compacted per-term cost.
- **Gate:** no query was timed until it passed.
  - It first blocked on a bug in its own rule: the deletion-marker field. That was fixed test-first.
  - Under the strict default it then refused this fixture after 900 s, as specified, because of the two small indexes.
- **Finding:** small single-batch indexes may never get a first cycle. This is observed, not explained; see [below](#small-indexes-without-a-first-cycle).

## Deployment

- **Server:** a dedicated fixture brought up with `go/flex-setup.sh` at 03:56:43Z.
  - Compose project `adbc-ssb-compacted`, ports 19543/19400, container `redis-adbc-flex`.
  - Compose override: `pull_policy: never` and a 20 GiB container limit without swap.
  - Image `redislabs/redis:latest` `679d117a56fc`, not re-pulled. It is the image the [compaction record](../flex-index-compaction/validation.md#deployment) identifies as Software 8.2.6-58.
  - Database: Redis 8.8.0, bigstore v2, `search_on_bigstore`, 1 shard, 64 GiB with an 8 GiB RAM tier, `noeviction`, TLS.
  - Search disk settings 8% / 2% / 3 MiB, `search-fork-gc-clean-threshold` 100, `search-fork-gc-run-interval` 30. AOF every second from 03:57:39, before loading.
- **QPF:** the management API reported active 4x QPF (6 Search workers, `mnp`, 32 connections) at 03:57:39, 04:35:05 (before timing) and 04:46:36 (after it).
- **Host:** Docker Desktop VM (16 CPUs, 31.5 GiB) on macOS 26.7.3, Apple silicon. Only this fixture and one client container ran during timing.
- **Client:** `adbc-olap-release-client:20261123`, 4 CPUs and 8 GiB without swap, through `--network container:redis-adbc-flex` (Linux namespace route, macOS host). The installed v0.0.31 wheel was pointed at the library with `REDIS_ADBC_LIBRARY`; metadata records the wheel's version, `0.0.31`. DuckDB 2.0.1, 4 client threads, shared client cgroup.
- **Build:** `go/build-linux.sh arm64 v0.0.32-main.ac82cfa0` in `redis-adbc-linux-builder:1.27.4-arm64` (c-shared, `-tags driverlib`, version linker flag). SHA-256 `ef18d2d3e50e58902538e2eabd1ced22671ccf8e51bb8be36d21a9de14473733`. The tree is main `ac82cfa0` plus a docs-only commit, with no Go changes.
- **Data:** the private profile's `ssb-sf1.duckdb` (SHA-256 `5e029600…`), copied into a dedicated profile with `adbc.redis.flex=true` and schema `ssb`.

## The gate on live data

1. **Load** (`run.py ssb --load-only`, 04:03:13Z). All five tables loaded and count-validated by 04:10:06.
   - The gate's first poll found every index uncompacted: `total_cycles` 0 and `disk_num_keys` = `num_docs` on every NUMERIC field.
   - At 04:10:36 `customer` had its cycle, but the gate still refused it. The driver's `__adbc_snapshot_deleted` field reports no `disk_num_keys` when no row is deleted, and the first rule treated that as unverifiable.
   - I added a failing test from that index's shape, fixed the rule, and stopped this run's gate wait at 04:11:42.
2. **Strict rerun** (`--load-only --reuse-loaded`, 04:12:30Z).
   - `customer`, `lineorder` and `part` passed as their cycles came.
   - `date_` and `supplier` never did. At 04:29:36 the gate refused after 900 s and 61 polls (`suite_error`, exit 1). No query was timed.
3. **Exempt run** (`supervise.py ssb --reuse-loaded`, 04:35:17Z).
   - The requesting session chose the opt-in exemption. The profile set `flex_compaction_exempt_max_docs` to 10,000 and gave `flex-index-created.json` the shard log's `FT.CREATE` times.
   - Setup passed on its first poll, with `exemption.applied_to` = `date_`, `supplier` and `not_needed` empty.
   - Each of the 13 queries rechecked before timing and passed.

## Compaction state per table

`FT.CREATE` and first-cycle times are from the shard log: "Successfully created index" and the first "Starting full compaction on inverted index column family" of each index's burst. The counts are from `FT.INFO`, as recorded in `metadata.json` and in every query record.

| Table | Rows | `FT.CREATE` | First GC cycle | State when timed | `total_cycles` | Largest NUMERIC `disk_num_keys` |
|---|---:|---|---|---|---:|---:|
| customer | 30,000 | 04:03:15.064 | 04:10:32.089 (7.3 min) | compacted | 1 | 1,514 |
| date_ | 2,556 | 04:03:16.434 | none by 04:46:36 (43.3 min) | **exempt, uncompacted** | 0 | 2,556 |
| lineorder | 6,001,171 | 04:05:37.197 | 04:14:18.201 (8.7 min; 18.6 s cycle) | compacted | 1 | 273,518 |
| part | 200,000 | 04:10:01.260 | 04:18:39.261 (8.6 min) | compacted | 1 | 14,797 |
| supplier | 2,000 | 04:10:04.975 | none by 04:46:36 (36.5 min) | **exempt, uncompacted** | 0 | 2,000 |

- **Compacted tables:** first cycles 7.3–8.7 minutes after `FT.CREATE`, within the 5.9–9.4 minutes of the [compaction record](../flex-index-compaction/validation.md#when-the-first-cycle-runs). `lineorder`'s largest NUMERIC field, at 273,518 keys, is within that record's 243,804–273,632.
- **Exempt tables, across the 13 query records:**
  - `date_` was 32.4–42.7 minutes past `FT.CREATE`, and `supplier` 25.6–35.9.
  - One point term (`d_datekey`, `s_suppkey`) took 0.22–0.76 ms in total, 0.15–0.60 ms of it in the `DISK_NUMERIC` iterator, with estimated matches 2,556 and 2,000.

## SSB SF1 on Flex

`supervise.py ssb --reuse-loaded`, direct Redis only, 2026-11-26 04:35:17–04:46:08Z. Each query is a warm-up and three timed runs under a 60 s statement deadline, fully fetched and validated against DuckDB. All 13 native DuckDB references succeeded (4.8–12.6 ms medians).

Median of the timed runs (warm-up; the three runs). Terms are the key-union terms pushed into `lineorder`, from the [demand analysis](../flex-ssb-key-demand/validation.md#what-each-query-pushes); `ac82cfa0` includes that change.

| Query | Terms | 2026-11-26, uncompacted (#691 build) | **main `ac82cfa0`, compacted with the exemption** |
|---|---:|---|---|
| Q1.1 | 12 | 9.1 s | **1.9 s** (3.2; 2.0/1.9/1.9) |
| Q1.2 | 1 | 0.9 s | **0.59 s** (0.89; 0.66/0.59/0.53) |
| Q1.3 | 1 | 0.7 s | **0.15 s** (0.18; 0.15/0.15/0.15) |
| Q2.1 | 301 | timeout | **25.0 s** (32.0; 25.0/25.1/24.9) |
| Q2.2 | 1,924 | timeout | **21.4 s** (22.3; 21.4/21.5/21.3) |
| Q2.3 | 514 | timeout | **5.1 s** (5.3; 5.3/5.1/5.0) |
| Q3.1 | 422 | timeout | **27.6 s** (30.1; 27.6/28.0/26.6) |
| Q3.2 | 1,343 | timeout | **14.1 s** (15.5; 14.0/14.3/14.1) |
| Q3.3 | 349 | timeout | **10.3 s** (10.6; 10.3/10.5/10.2) |
| Q3.4 | 278 | timeout | **6.9 s** (7.2; 6.9/6.9/7.2) |
| Q4.1 | 301 | timeout | **28.8 s** (31.1; 29.9/28.8/27.6) |
| Q4.2 | 325 | timeout | **6.9 s** (8.0; 6.9/7.0/6.7) |
| Q4.3 | 95 | 57.6 s | **2.1 s** (2.5; 2.4/2.1/2.0) |

- **Validated:** 13 of 13, against 4 of 13. RAM with v0.0.31 validated all 13 (`PERFORMANCE.md`).
- **Comparability:** the earlier run used another server, whose `lineorder` index was never compacted, and a #691 build from the working tree (`f7c91495`). Here both the driver and the compaction state differ. The 2026-11-26 column comes from [that record](../flex-ssb-key-demand/validation.md#ssb-sf1-on-flex); this run does not reproduce it.
- **The slowest queries, Q2.1, Q3.1 and Q4.1 (25–29 s),** have supplier or date demands that match 1.1M–5.5M `lineorder` rows. Q2.2 has the most terms and takes 21.4 s. So the remaining time does not follow the term count alone. Not profiled.
- **Q4.3** went from 57.6 s, with 1.4–3.7 s to spare, to 2.1 s.

## Small indexes without a first cycle

**Observed:**
- `date_` and `supplier` stayed at `total_cycles` 0, with `disk_num_keys` equal to `num_docs` on every NUMERIC field, through the last check at 04:46:36. That was 43.3 and 36.5 minutes after their `FT.CREATE`.
- **Same database, same load:** the other three indexes were loaded the same way by the same process and compacted at 7.3–8.7 minutes.
- **One batch each:** each small index was loaded in one 4,096-row Arrow batch, in 0.18 s and 0.10 s. The others took 8, 49 and 1,466 batches.
- **Above the threshold:** each small index had 2,000 or more indexed documents, more than the `search-fork-gc-clean-threshold` of 100.
- **No consistency window:** the shard log has one AOF rewrite at 03:57:38 ("SAVE end mode=FULL_RDB sst=false"), before loading. After it there is no SAVE, rewrite, checkpoint or pause line. The database was new and was not restarted.

**Not explained:**
- **Source:** in the public RediSearch 8.8-rse source (`spec.c`, `gc.c`, `disk_gc.c`, `indexer_internal.h`), the GC context starts with the index, each indexed document counts as a write, and a cycle runs once writes, deletes and updates reach the threshold while background work is not paused. By that logic both indexes should have had a cycle. The scheduling is partly in the closed disk implementation.
- **Earlier tests:** the [compaction record](../flex-index-compaction/validation.md#when-the-first-cycle-runs) never went below 20,000 documents, and every index there compacted in 5.9–9.4 minutes.
- **Single batch:** that each small index was written in one batch is the visible difference, not an established cause. It was not reproduced without the driver.

**Impact:**
- **On queries:** small. One point term costs 0.2–0.8 ms on these indexes.
- **On tooling:** a gate that waits for `total_cycles` ≥ 1 never finishes. Clients cannot start the cycle on Redis Software (`FT.DEBUG` is not available there).
- **Relation to the earlier server:** it may be the same gap as the 1.5M- and 6.0M-document indexes that stayed uncompacted for hours on the earlier server ([compaction record](../flex-index-compaction/validation.md#not-established)). Not established.

## Tests

- **Harness:** `python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py' -v` gives 64 tests, all passing, with 1 skipped: the opt-in cgroup OOM test, which needs a disposable privileged container.
- **New tests:** `test_flex_compaction.py` has 30. Before the harness wiring, the 6 harness-level tests failed (setup records nothing; the query path neither rechecks, refuses nor records), while the 17 module tests passed.
- **Fixes found on live data**, each with its failing test first:
  - `test_numeric_field_without_disk_entries_does_not_block`: the deletion-marker shape.
  - The six `ExemptionTests`: missing exemption limit, state, evidence, drop-when-compacted and per-query record.
- **Fixtures:** test replies use the RESP3 `FT.INFO` and `FT.PROFILE` shapes this server returned to `redis-py` 8.1.0.

## Not established

- Why the two small indexes never got a first cycle, and whether a driver-free single-burst load reproduces it.
- Why Q2.1, Q3.1 and Q4.1 take 25–29 s once compacted. Not profiled.
- **Variance:** one run, no cold-cache series and no repeat on a second fixture.
- **Other deployments:** more shards, a larger RAM tier, Redis Cloud, other Software or Search versions.
- **TPC-H on Flex:** not run. With the gate, TPC-H `nation` (25 rows) and `region` (5) fall below the GC threshold and do not block.
- **Review:** human review is pending.

## Fixture cost and ownership

Owned by this session: local Docker on the developer's Mac, trial license, no cloud resources, no cost. The API still reported active 4x QPF at 04:46:36. The container and its Compose network were removed at 04:47:04Z (`docker compose --profile flex down`); the service has no volumes. Raw results, events and a shard-log excerpt stay in the worktree's git-ignored `go/build/ssb-compacted-results/`.
