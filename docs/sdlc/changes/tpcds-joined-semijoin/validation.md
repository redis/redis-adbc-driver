# Validation and review

Date: 2026-11-25. Baseline: `252b01a4`, rebased onto `af305298` (#687).
Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Environment

Same TPC-DS SF 0.1 fixture and method as [tpcds-inner-before-outer](../tpcds-inner-before-outer/validation.md):
- Redis Software (Redis 8.8.0) RAM database, 3 shards, authenticated TLS. 4× QPF verified: active, scaling factor 4, 6 Search workers, `mnp` routing, 32 connections.
- DuckDB 2.0.1 data and reference results; all 24 table counts checked.
- macOS client, single cold runs with a full fetch, multiset comparison with the harness tolerance.

## Diagnosis on the baseline

Q35 timed out at 120 s. The subquery counters recorded 2,492 per-row body runs and no semi-join. Execution diagnostics showed 811 MB read and 106 Redis worker-seconds. A CPU profile showed `evalSubquery` → `subqueryRows` → `snowflakeJoinRows` → `scanParallelStarFacts`. Allowing joined bodies with an unconditional up-front build gave Q35 3.5 s, Q69 1.0 s and Q10 0.8 s, each with three semi-joins and no per-row runs.

## Same-fixture A/B against the baseline

The subquery queries and two controls, alternating the two libraries twice:

| Query | Baseline (s) | This change (s) |
|---|---|---|
| Q35 | 60.0 / 60.0, timeout | 2.57 / 2.58 |
| Q69 | 33.8 (34.2 on a rerun) | 0.89 / 0.84 |
| Q10 | 4.2 / 3.9 | 1.03 / 1.00 |
| Q16 | 0.29 / 0.27 | 0.28 / 0.28 |
| Q23 | 8.1 / 8.0 | 7.9 / 7.9 |
| Q94 | 0.13 | 0.11 / 0.12 |
| Q2 | 5.3 / 5.0 | 5.3 / 5.3 |
| Q13 | 2.9 / 2.8 | 2.9 / 3.1 |

The before-rebase branch, which lacked #687, validated 98 of 99 queries; the one left was Q72, which #687 fixes.

The build rebased onto #687 validated **99 of 99**. The 94 queries the original `main` validated took 219.5 s in total, against 232.4 s on that `main`. All 99 took 227.5 s; the median was 1.3 s and the 90th percentile 5.8 s. The slowest were Q78 29.4 s, Q80 17.6 s, Q76 14.1 s, Q23 9.7 s, Q93 8.2 s, Q4 7.4 s and Q14 7.2 s. These are the likeliest to exceed 60 s at SF1.

## Tests

- `TestSQLSemiJoinJoinedBodies` failed on the baseline: every case ran per row, with no semi-join. With the change it passes. It compares each case with the per-row oracle and covers comma and explicit joins, NOT EXISTS, the Q35 shape (3 semi-joins), a LEFT JOIN body with two keys, IN, an outer reference in ON (stays per row) and the adaptive build.
- The full Go suite on a fresh 3-shard 4× QPF fixture failed only `TestSQLSemiJoins`. That test listed "a join in the subquery" as a per-row shape; its rows were unchanged and it now ran as one semi-join. The case is now asserted as a semi-join with the same rows.
- After that update, all 20 semi-join, EXISTS and subquery integration tests pass.
- `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages OK.

## Limits

SF 0.1 in RAM mode only. The adaptive build's estimate assumes star-shaped bodies, as described in the [spec](spec.md). No SF1, Flex or Cluster API run was done. Human review is pending.
