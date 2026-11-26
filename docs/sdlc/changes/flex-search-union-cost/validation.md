# Validation

2026-11-25; branch from main `16032de1`, rebased onto `064eb12d`, then `c8f6f50d` (#686–#688). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **Q7:** on SF1 Flex it now takes 14.1–16.2 s, against 30.4–36.6 s on main in the same window. The saving is two of the three Search evaluations of its lineitem filter, about 8 s each.
- **Q2:** it takes 13.7–14.1 s again, against 94–97 s on main. Its correlated subquery no longer pushes a 1,987-key demand for each of 747 parts.
- **Rows and validation:** every run was fully fetched and validated, and read the same rows as main.

## Deployment

- **Server:** Redis Software 8.8.0 Flex (bigstore v2, `search_on_bigstore`), 1 shard, 64 GiB database with an 8 GiB RAM tier.
  - Search disk settings 8% / 2% / 3 MiB per index.
  - Management API verified active 4x QPF: 6 Search workers, `mnp`, 32 connections.
  - `noeviction`, TLS, AOF every second, 20 GiB server container cap, no swap.
  - Provisioned with `go/flex-setup.sh` on the image already used for #643–#666 (no new pull). Every setting the earlier runs recorded matched.
- **Client:** one Linux ARM64 container at a time, 4 CPUs / 8 GiB, no swap, through `--network container:redis-adbc-flex` (Linux namespace route; macOS host). Its image is the one used for #643–#666.
- **Host:** the Docker VM (16 CPUs, 31.46 GiB) ran nothing else during these measurements.
- **Data:** TPC-H SF1 from the checksum-verified source of the earlier runs, loaded through the harness with the main build. All 8 tables count-validated; lineitem took 451 s.
  - After loading: 8.66M keys, 12.8 GB used, about 7.1M objects in RAM and 6.1M on flash.
- **Builds:** each built with the Makefile's c-shared flags (`-tags driverlib`, version linker flag) in the repository's Linux ARM64 builder image. Library SHA-256 prefixes:
  - main `16032de1` (newest main when the work started): `c865106b`.
  - main `064eb12d` (newest main after #683–#685 merged): `d1943a44`.
  - Intermediate builds on `16032de1`, not the final change: count reuse alone `85bbdbaa`, fetch first alone `0f77d4c9`.
  - **This change, on `064eb12d`:** `ec316117`.
  - The Python package is unchanged since v0.0.31, so `REDIS_ADBC_LIBRARY` pointed the client's installed wheel at each library.

## Search cost of key unions on Flex

A read-only probe (scratch Go program, not committed) issued the driver's union shape, `(@c:[k k] | …)`, with `NOCONTENT LIMIT 0 0 TIMEOUT 0 DIALECT 2`. It used the SF1 keys each query would demand. Times are warm repetitions; the first call of each was slower.

| Column | Terms | Matches | Time |
|---|---:|---:|---:|
| `l_partkey` (Q9's green parts) | 100 | 2,922 | 204 ms |
| `l_suppkey` (Q7's suppliers) | 100 | 60,007 | 405 ms |
| `l_suppkey` | 798 | 478,523 | 7.9 s |
| `l_suppkey` AND `l_shipdate` 1995–1996 (Q7's lineitem filter) | 798 | 145,703 | 7.7–8.2 s |
| `o_custkey` (Q7's customers) | 1 | 0 | 137 ms |
| `o_custkey` | 100 | 940 | 13.8 s |
| `o_custkey` | 1,000 | 10,157 | about 150 s |

- **Per term:** about 2 ms for `l_partkey`, 4–10 ms for `l_suppkey` and 137 ms for `o_custkey`, even for a key with no orders. RAM measured about 45 µs per term (#677).
- **Q7's filter:** each evaluation of its lineitem candidate query costs about 8 s.
- **Fetch:** fetching all 478,523 keys of the 798-term union took 8.1 s, against 7.9 s to count it.

### Rejected: a wider Flex key demand

#665 suggested pushing larger key demands: Q7's 12,008 customers into orders (121,324 of 1.5M orders), or Q9's 10,664 green parts into lineitem (319,404 of 6M rows). A first change let any filtered dimension's demand reach 16,384 keys. At the costs above:
- **Q7:** the customer demand would take about 27 minutes per evaluation. Reading all 1.5M orders takes about 7–33 s, depending on how many are in the RAM tier.
- **Q9:** the part demand would take about 21 s per evaluation. With this change's single evaluation, that could beat reading all 6M lineitem rows when they are cold (about 45k rows/s), but not when they are warm.

The cost per term differs about 70× between columns and is not known when the plan is made. So a blanket wider limit was reverted and is not part of this PR. A cost-aware variant, or the TAG key mirror of #677, would be separate work.

## Q7 on SF1 Flex

`tpch_tool.py one` (scratch, not committed): each process ran Q7 twice (300 s deadline), fully fetched, then validated the result against the SF1 DuckDB source. The builds were interleaved in fresh processes, 2026-11-25 19:42–19:52Z. Each process used 32 workers (the Flex default), 570 shared-candidate chunks, and read 1,658,513 rows.

| Build | Process 1 | Process 2 | Process 3 |
|---|---|---|---|
| main `16032de1` | 34.9, 31.3 s | 32.8, 36.6 s | 31.5, 30.7 s; then 30.8, 30.4 s |
| count reuse alone | 25.7, 24.1 s | 22.9, 22.8 s | 22.9, 23.2 s; then 22.1, 22.7 s |
| fetch first alone | **16.2, 14.1 s** | **14.3, 14.7 s** | — |

- **Count reuse alone** removed one of the three evaluations: about 8 s.
- **Fetch first** removed the second: about 8 s more.
- **Rows read** were the same in every run, and **Redis commands** differed by under 20.

### Over-budget overhead

When a reader's filter is over the shared budget, its single fetch also returns up to 524,288 row keys, which are discarded. Measured with `redis-cli` inside the server container, on Q3's lineitem filter (`l_shipdate > 1995-03-15`, 3,241,776 matches):
- `LIMIT 0 0` took 2.9 s warm (5.3 s cold);
- `LIMIT 0 524288` took 3.2 s.

So such a reader pays about 0.3 s more, plus decoding the keys on the client. A selective filter saves a full evaluation.

## Q2: correlated key demand

In the full SF1 Flex pass on main `16032de1` (below), Q2 timed out at 60 s in "failed complete snapshot candidate read". Run alone with diagnostics (300 s deadline), each result fully fetched and validated (100 rows):

| Build | Seconds | Rows read | Redis commands | Bytes written | Socket read worker-seconds |
|---|---|---|---|---|---|
| main `fdd49f7c` (before #643/#644) | 13.9, 13.7 | 927,508 | 960,367 | 220 MB | 10.2 |
| main `16032de1` | 97.1 (again 94.3) | 923,964 | 956,935 | 314 MB | 76.9 |
| fetch first only | 95.4 | 923,964 | 957,839 | — | — |
| **this change** (on `064eb12d`) | **14.1, 13.7** | 925,162 | 958,943 | — | — |

- **Same plan, same rows:** every build ran the streaming snowflake join and read the same rows with nearly the same command count. On `16032de1` it wrote 94 MB more and waited about 67 more worker-seconds on replies.
- **The server's side:** during one `16032de1` run, the server handled 5,927 more `FT.SEARCH` calls, 1,819 of them over 10 ms, about 61 s of Search time.
- **The slow calls:** the slowlog showed them all with one shape, `@ps_partkey:[k k] (@ps_suppkey:[…] | …)`, a 1,987-term union, at 32–36 ms each, three to four per part. This is Q2's correlated `min(ps_supplycost)` subquery, run once for each of 747 parts. Since #644 its 1,987 EUROPE suppliers fit the Flex demand limit (4,096). Before, they were over 1,000 and no demand was built.
- **Not the union itself:** the same unions alone are cheap on `partsupp`: 100 `ps_suppkey` terms take 19 ms and 100 `ps_partkey` terms 9 ms. The cost is their repetition.

## Full TPC-H SF1 suite

`benchmarks/olap-release/supervise.py tpch --reuse-loaded --include-duckdb-adbc`, three times, 2026-11-25:
- main `16032de1`: 19:54–20:49Z.
- this change on `064eb12d`: 21:12–22:06Z.
- main `064eb12d`: 22:06–22:58Z.

Each query is one fresh coordinator: native DuckDB, then direct Redis, then DuckDB over Redis ADBC. Each engine runs a warm-up and three fully fetched timed runs under a 60 s statement deadline, and is validated separately against the DuckDB reference. All 22 queries ran in each pass, and every pass had a client OOM-kill delta of 0.

Direct Redis ADBC, median of the timed runs (warm-up; the three runs):

| Query | main `16032de1` | main `064eb12d` | **this change** | DuckDB median |
|---|---|---|---|---:|
| Q1 | timeout | timeout | timeout | 18.7 ms |
| Q2 | timeout | timeout | 14.9 s (15.5; 14.6/15.0/14.9) | 3.6 ms |
| Q3 | timeout | timeout | timeout | 20.2 ms |
| Q4 | timeout | timeout | timeout | 8.4 ms |
| Q5 | 47.1 s (54.2; 47.1/45.1/50.3) | timeout | 48.1 s (55.3; 49.5/47.5/48.1) | 13.4 ms |
| Q6 | 4.5 s (7.6; 4.6/4.3/4.5) | 5.2 s (8.1; 6.0/4.8/5.2) | 4.5 s (7.3; 5.1/4.5/4.3) | 4.4 ms |
| Q7 | 34.0 s (38.8; 35.0/34.0/32.9) | 34.6 s (36.5; 35.1/34.6/33.9) | 24.1 s (17.3; 17.1/24.1/29.3) | 17.8 ms |
| Q8 | 54.3 s (55.4; 54.3/54.3/54.0) | 55.1 s (56.1; 55.8/55.1/54.8) | 22.2 s (23.3; 22.5/22.0/22.2) | 10.4 ms |
| Q9 | timeout | timeout | timeout | 30.0 ms |
| Q10 | timeout | timeout | timeout | 19.0 ms |
| Q11 | 1.2 s (2.0; 1.3/1.2/1.2) | 1.2 s (1.6; 1.2/1.2/1.2) | 0.9 s (1.9; 0.9/0.9/0.8) | 4.9 ms |
| Q12 | 13.9 s (18.2; 14.3/13.4/13.9) | 12.8 s (17.1; 15.8/12.8/12.2) | 14.2 s (24.2; 17.1/14.2/13.6) | 9.3 ms |
| Q13 | 13.5 s (13.6; 14.4/13.5/12.9) | 13.6 s (15.7; 15.4/13.5/13.6) | 16.4 s (19.4; 16.4/14.7/16.4) | 45.4 ms |
| Q14 | 2.9 s (5.6; 3.0/2.9/2.9) | 2.9 s (9.3; 3.1/2.9/2.9) | 1.9 s (8.5; 2.0/1.8/1.9) | 8.6 ms |
| Q15 | 20.5 s (47.7; 23.7/20.3/20.5) | 22.5 s (33.5; 22.9/22.5/21.3) | 23.3 s (47.6; 24.2/22.1/23.3) | 5.9 ms |
| Q16 | 4.9 s (31.1; 5.0/4.8/4.9) | 6.2 s (33.0; 6.2/6.3/5.8) | 7.1 s (37.2; 7.1/5.9/7.2) | 14.2 ms |
| Q17 | 15.7 s (16.3; 15.7/15.5/15.8) | 15.8 s (16.4; 15.8/16.4/15.8) | 12.9 s (13.9; 13.4/12.8/12.9) | 9.5 ms |
| Q18 | 44.1 s (46.7; 44.3/44.1/43.6) | 44.5 s (46.7; 44.8/44.5/43.8) | 49.1 s (57.5; 50.1/47.9/49.1) | 31.5 ms |
| Q19 | 3.0 s (4.1; 3.2/3.0/2.8) | 3.3 s (4.8; 3.6/3.3/3.0) | 3.0 s (4.9; 3.0/3.3/2.4) | 8.9 ms |
| Q20 | timeout | timeout | timeout | 13.8 ms |
| Q21 | timeout | timeout | timeout | 22.7 ms |
| Q22 | 9.6 s (9.4; 9.9/9.6/9.6) | 9.8 s (10.7; 10.2/9.8/9.2) | 11.9 s (13.1; 11.9/12.8/11.0) | 11.1 ms |

- **Validated:** 14, 13 and **15** of 22.
- **Q2** passes only with this change.
- **Q7 and Q8** are 10 s and 33 s faster. Q8's warm-up now leaves 37 s of margin instead of 4.
- **Q5** sits at the deadline on every build (warm-ups of 54–55 s), and timed out once on main `064eb12d`.
- **The other differences** are within the run-to-run drift below.
- **Every build times out on** Q1, Q3, Q4, Q9, Q10, Q20 and Q21.

DuckDB over Redis ADBC validated the same six queries in every pass: Q2, Q11, Q13, Q15, Q16 and Q22, with medians of 8.9–47.3 s. The other 16 timed out.

### Drift check

The suite put Q13, Q16, Q18 and Q22 2–5 s slower on this change than on both mains. Q13, Q18 and Q22 were rerun interleaved on main `064eb12d` and this change, each in a fresh process twice, fully fetched and validated:

| Query | main | this change | main | this change |
|---|---|---|---|---|
| Q22 | 10.7, 10.0 s | 9.8, 10.3 s | 10.4, 10.2 s | 9.4, 10.0 s |
| Q13 | 11.1, 13.6 s | 13.3, 13.3 s | 14.2, 13.6 s | 13.2, 13.2 s |
| Q18 | 112.2 (cold), 47.9 s | 47.1, 46.3 s | 47.7, 44.4 s | 45.4, 48.3 s |

Both builds sent the same commands (within 8 of 6.15M for Q18) and the same bytes, so these queries do not use the changed path. The suite's differences were drift. Q18's cold first run reads all 6.15M lineitem rows from flash, like Q1.

## SSB SF1 (#658)

**Setup.** SSB SF1, from the checksum-verified source of the v0.0.31 run, was loaded into schema `ssb` of the same Flex database (`adbc.redis.default_schema=ssb`; SSB's table names collide with TPC-H's). The load used main `064eb12d`; lineorder took 442 s, and all tables were count-validated.

**Runs.** `supervise.py ssb --reuse-loaded`, direct Redis only, with the same 4-CPU / 8 GiB client as TPC-H. The v0.0.31 RAM run used a 12 GiB client. Each run was fully fetched and validated. There were no client OOM kills.

| Query | RAM v0.0.31 (`PERFORMANCE.md`) | Flex, main `064eb12d` | Flex, this change |
|---|---:|---|---|
| Q1.1 | 1.5 s | timeout | timeout |
| Q1.2 | 0.7 s | timeout | 19.6 s (20.9; 20.2/19.2/19.6) |
| Q1.3 | 0.5 s | 12.9 s (14.5; 12.9/13.0/12.9) | 4.5 s (4.6; 4.6/4.5/4.4) |
| Q2.1 | 20.8 s | timeout | timeout |
| Q2.2 | 31.4 s | timeout | timeout |
| Q2.3 | 0.3 s | timeout | timeout |
| Q3.1 | 29.4 s | timeout | timeout |
| Q3.2 | 1.9 s | timeout | timeout |
| Q3.3 | 0.6 s | timeout | timeout |
| Q3.4 | 0.6 s | timeout | timeout |
| Q4.1 | 32.1 s | timeout | timeout |
| Q4.2 | 27.7 s | timeout | timeout |
| Q4.3 | 3.1 s | timeout | timeout |

**Search cost on `lineorder`.** Each SSB key matches about 2,400–3,000 lineorder rows, and on this deployment a NUMERIC key term costs about 0.6 s per evaluation:
- `lo_suppkey`: 100 of the 378 AMERICA suppliers took 58–62 s.
- `lo_orderdate`: 100 of 1993's dates took 55 s.

Coalesced into one range per month, the same 365 dates of 1993 took 7.5 s as 12 terms (908,288 matches). So a range costs about 8 µs per matching row, against about 240 µs as single keys.

Most SSB queries push a supplier or customer key demand. On this deployment, one evaluation of that demand exceeds the 60 s deadline, and reading all 6M lineorder rows instead would take about 26–133 s at the warm and cold read rates measured for #643 (about 230k and 45k rows/s on 32 connections).
- **This change:** one evaluation instead of three is enough for Q1.2 and Q1.3.
- **Not attempted here:** pushing consecutive date keys as ranges (Q1.x), or skipping an unfiltered dimension's demand (Q2.x pushes all 2,556 dates). Both are separate planner changes.

## TPC-H Q10 and Q20 (#658)

Each was run alone with diagnostics on this change (120 s deadline), together with the server's Search statistics.
- **Q10:** `parallel_streaming_star_join` on 32 workers. It had read 3.77M rows when the deadline hit, and workers spent about 75% of their time waiting on replies (2,891 worker-seconds). It made only 4 Search calls (10.6 s): its `l_returnflag = 'R'` lineitem filter is over the candidate budget, so lineitem is read by exact-directory ranges. **Bound:** row reads.
- **Q20:** `select_scan` on `partsupp`, no workers. Its correlated `sum(l_quantity)` subquery runs once per `partsupp` row, each run a Search such as `@l_partkey:[75964 75964] @l_suppkey:[979 979] @l_shipdate:[…]`. That was 10,608 Search calls and 78 s of Search time in 120 s. **Bound:** the per-row correlated subquery.

## Tests

- **Changed:** `TestParallelStarSharesFlexCandidatesAcrossWorkers` now requires exactly one Search call. On main it makes three; the count-reuse step alone makes two.
- **Added:**
  - `TestParallelStarSharedFlexFetchRejectsShortReply`: a fetch reply with 1,999 keys and a total of 2,000 fails with "incomplete snapshot candidate materialization", with no rows, no ranges and no HASH reads.
  - `TestParallelStarSharedFlexLimitRefusalCountsThenFetches`: with Search's limit at 100,000, the refused fetch is followed by one count and one fetch of exactly that count (three calls, one fetch), with the same rows.
- **Correlated limit:**
  - `TestFlexCorrelatedFactKeyDemandKeepsUnionLimit` (unit): with `outer` set, a 1,500-key Flex demand is neither built nor pushed. Without the change, it fails.
  - `TestSQLFlexCorrelatedJoinKeepsDemandLimit` (Redis): a Q2-shaped correlated minimum, with 1,360 of 2,000 suppliers in the region and `partsupp` as the streamed fact. Its 30 correlated runs made 4 Search calls with a union of over 1,000 terms, all from the outer join, and returned the eager join's rows. A hook on the execution pool counts the calls. Without the change the same test sees 124 and fails.
- **Unchanged and passing:** `TestParallelStarProvenOverBudgetFlexUsesOnlyCountThenRanges` (one Search call, then ranges), the ambiguous-count and admitted-selective tests, and the Flex snapshot candidate and budget tests.
- `gofmt` clean. `go vet` reports only the existing copied-lock warning in `live_ingest_creation_integration_test.go`.
- Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...` passes (all 7 packages), on `064eb12d` and again after rebasing onto `c8f6f50d`.
- **Integration fixture:** a dedicated Redis Software 8.8.0 database brought up with `software-setup.sh`: 3 shards, API-verified active 4x QPF, own Compose project and ports, image not re-pulled.
- **Integration results:** `-run 'Flex|Snowflake|Star|Candidate|Demand|Join|Q19|SemiJoin|Union|InList|Subquery|Correlat|Partition|Aggregate' -skip 'LiveHash|Retained|Snapshot'` gave 260 passed, 39 skipped, 0 failed. The skips are suites that need a dedicated snapshot database.
- **Snapshot results:** on the same disposable fixture as `REDIS_SNAPSHOT_URI`, with `-race`, `-run 'Snapshot.*(Flex|Candidate|Partition|Aggregate|Star|Join|Directory|Tuple)|TestSnapshotPartitioned'` gave 52 passed, 6 skipped, 0 failed. The four-shard CI snapshot matrix was not run locally.

## Not established

- **Cold-cache series:** no controlled series per query. Results depend on what the 8 GiB RAM tier holds, and Q5 and Q18 sit near the deadline.
- **Other deployments:** a larger RAM tier, or more shards.
- **SSB plans:** the SSB plan changes outlined above (date ranges, unfiltered-dimension demand) were not attempted.
- **CI snapshot matrix:** the full four-shard matrix runs in CI only.
- **Newest base:** the SF1 runs and the integration and snapshot tests used the `064eb12d` base. #686–#688 (TPC-DS join planning) merged afterwards. On `c8f6f50d` only the unit suite was rerun.
- **Review:** human review is pending.
