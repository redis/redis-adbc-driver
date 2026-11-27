# Validation

2026-11-26 (UTC). Base: main `5adb8027`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **SF1 on Software Flex,** both queries now validate:
  - **Q93:** OOM-killed before; now 7.54 s.
  - **Q49:** timed out before; now 6.49 s.
- **All 99 TPC-DS queries** validate at SF 0.1 with the change.
- **Same-fixture A/B** at SF 0.1: queries the rule cannot reduce run at the same speed as on main.

## Unit tests (no Redis)

`outer_join_reduce_test.go`, on the in-memory TPC-H-shaped fixture of the snowflake tests. Its `LEFT JOIN orders` adds NULL rows for lineitems without an order, and some orders have a NULL `o_custkey`.

- **`TestReduceOuterJoinsPlan`:**
  - Reduced: 11 strict `WHERE` forms (equality, join to another item, one conjunct of several, `BETWEEN`, `IN` list, `IS NOT NULL`, date comparison, arithmetic, `NOT`, `CAST`, an `OR` of strict sides).
  - Kept `LEFT`: 8 non-strict forms (`IS NULL`, `COALESCE`, an `OR` with the preserved side, a preserved-side-only filter, `CASE`, `IS DISTINCT FROM`, a subquery), and any `WHERE` when the FROM clause has a `FULL` join.
- **`TestReduceOuterJoinsResults`:** every one of those queries returns the same rows with and without the rule (`reduceOuterJoinsEnabled`).
- **`TestReduceOuterJoinsAdmitsStreamedJoin`:** a Q93-shaped `lineitem LEFT OUTER JOIN orders …, customer WHERE o_custkey = c_custkey …` is admitted by `planSnowflakeShape`, runs as `streaming_snowflake_join` and matches the eager result.
- **Before the rule,** with only the test toggle added, all 11 reducible plan cases and the admission test failed, as expected. The non-reducible and result cases passed.
- **Unit-only suite:** `cd go && env -u REDIS_URI -u REDIS_FLEX_URI go test -count=1 -p 1 ./...` passes; exit 0.
- **Formatting:** `gofmt -l` on the changed files reports nothing.

## Deployment

**Fixture:** one dedicated fixture from `go/flex-setup.sh`:
- Redis 8.8.0 Flex (bigstore v2, Search on Flex), 1 shard, 8 GiB RAM tier of 64 GiB, `noeviction`, TLS, AOF every second, Search disk settings 8/2/3 MiB.
- Active 4x QPF (6 Search workers, `mnp`, 32 connections), verified through the management API before and after every run.
- TPC-DS SF1 (19,557,375 rows) in schema `tpcds` and SF 0.1 (1,176,170 rows) in schema `tpcds01`, both managed tables, count-validated and gated for index compaction as in the [SF1 record](../sf1-flex-suites/validation.md).
- **Shared host:** during these runs another session's Redis containers also ran on the Docker host.

**Client:** 4-CPU / 8 GiB Linux client in the server's network namespace (macOS Docker Desktop host), `adbc.redis.flex=true`, 60 s statement deadline.

**Libraries:**
- main: `717b20d9…`, built from `7c83c486`; its Go sources are those of `5adb8027`.
- main plus the #696 status fix: `727b51e2…`. The fix does not change planning; it is the source of the SF1 "before" diagnostics.
- This change: `e82561d3…`.

## TPC-DS SF1 on Flex

Harness focused runs: one warm-up and three timed runs, each fully fetched and validated against DuckDB. Diagnostics are the harness's warm-up diagnostics.

| Query | Before (main planning) | This change |
|---|---|---|
| Q93 | OOM-killed 33.7 s into the warm-up; `select_scan`, 3.17M rows processed, 7.1 GiB heap | **7.54 s** median (warm-up 12.75 s; 7.40 / 7.54 / 8.48 s); `parallel_streaming_star_join`, 32 workers, 16,981 rows, 0.19 GiB allocated; 100 rows |
| Q49 | timeout in the warm-up (61.4 s); `select_scan`, 1.81M rows processed | **6.49 s** median (warm-up 10.87 s; 7.20 / 6.49 / 5.95 s); `select_scan`, 2,534 rows; 32 rows |

## TPC-DS SF 0.1 on Flex

`supervise.py tpcds --reuse-loaded` with this change: **99 of 99 validated**, no worker failures. The compaction gate waited 497 s; customer_address, household_demographics, item, promotion and web_returns were exempt.

The suite's summed medians (116.4 s) were higher than the [SF 0.1 record](../sf01-flex-suites/validation.md) on main (86.0 s), which ran on an otherwise empty fixture. A same-fixture A/B (harness focused runs, alternating the libraries) shows the difference is the fixture, not the change:

| Query | main | change | main | change |
|---|---:|---:|---:|---:|
| Q78 | 8.58 s | 9.07 s | 9.78 s | 8.02 s |
| Q80 | 4.03 s | 4.04 s | 4.21 s | 3.98 s |
| Q72 | 3.09 s | 2.98 s | 3.19 s | 3.02 s |
| Q75 | 3.25 s | 3.12 s | 3.19 s | 3.23 s |
| Q40 | 1.09 s | 1.05 s | 1.09 s | 1.06 s |
| Q49 | 1.62 s | **0.18 s** | 1.56 s | **0.18 s** |
| Q93 | 1.43 s | **0.03 s** | 1.44 s | **0.03 s** |

Q40, Q72, Q75, Q78 and Q80 have outer joins that are not reduced, because their `WHERE` is not strict in the NULL-supplying table.

## Not covered

- **True outer joins** (Q40, Q75, Q78, Q80 and their issues): the streamed join still does not NULL-extend.
- **The integration suite and the PostgreSQL differential corpus** were not run locally; see the PR's CI.
- **Review:** human review is pending.
