# Validation

2026-11-25; stacked on #643 (`claude/tpch-flex-read-path`, main `12f46745`). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.** On SF1 Flex, Q8 now meets the 60-second deadline in every measured run, 17.4–38.2 s. Q7 is not reliably under 60 s: 31.1 s once, then 61.2–82.6 s and one 60 s timeout.

## Tests

- **Shared candidates (unit):** `TestParallelStarSharesFlexCandidatesAcrossWorkers` uses 2,000 Flex candidates on two workers. It sees exactly three Search calls (count, count, fetch) and no directory range reads, reads each candidate once, and drops a candidate whose row no longer matches. Copied onto #643's tree, it fails: the fact ran serially (grant 0).
- **Shared candidates (integration):** `TestSQLFlexSnowflakeSharedCandidates` runs a Flex snowflake on Redis. Its execution plan reports `complete_flex_candidates`, and its rows equal the eager join's, both on published tables and after an UPDATE and a DELETE. On #643's tree it fails: the plan read exact-directory ranges.
- **Demand limit:** `TestFlexFactKeyDemandUnionLimit`.
  - With Flex, a 1,500-key demand is built and pushed as a 1,500-term union.
  - Without Flex, and for an unmarked OR, it is refused.
  - A demand over 4,096 keys is refused even with Flex.
- **Updated policy tests:**
  - `TestParallelStarProvenOverBudgetFlexUsesOnlyCountThenRanges` sets the shared budget to the scan budget, so its single count, then ranges, assertion is unchanged.
  - `TestParallelStarAdmittedFlexStillKeepsSelectivePath` now uses a set below two chunks, which keeps the serial candidate path.
- **Existing demand tests** pass `maxUnionTerms` explicitly.

Results:
- `gofmt` clean.
- `env -u REDIS_URI go test -count=1 -p 1 ./...` passes (all 7 packages).
- On the shared Redis Software fixture (8.8.0, 1 shard, verified 4x QPF, TLS), `-run 'Flex|Snowflake|Star|Candidate|Demand|Join|Q19|SemiJoin|Union|InList' -skip 'LiveHash|Retained'` gave 172 passed, 0 failed and 29 skipped. The skips are suites that need a dedicated snapshot database. This includes `TestSQLFlexSnowflakeSharedCandidates`, `TestSQLSnowflakeJoinsManaged` and `TestSQLFlexSemiJoinBuildsInnerOnce`.

## Subset (10% TPC-H, local RAM fixture, Linux namespace route, 4 CPUs / 8 GiB client)

Each query was fully fetched and validated against DuckDB.

| Mode | Q7 | Q8 | Q3 | Q5 | Q9 |
|---|---|---|---|---|---|
| `adbc.redis.flex=true` | 23.9 s | 10.7 s | 5.8 s | 3.1 s | 13.9 s |
| RAM | 3.9 s | 6.3 s | — | — | — |

All runs validated. The subset tables were created on the RAM profile and have no plain Flex index. In Flex mode they therefore check results and the demand's pushdown, but not the shared-candidate path. The host was shared with other sessions' containers, so times are observations.

## SF1 Flex

**Deployment and client.** The same deployment and client as [#643's validation](../tpch-flex-read-path/validation.md#sf1-flex):
- Redis Software 8.8.0 Flex, 1 shard, 8 GiB RAM tier, verified 4x QPF.
- One 4-CPU / 8 GiB Linux client through `--network container:redis-adbc-flex`.
- Exclusive and read-only, 2026-11-25 01:27–01:48Z, after the window for #643.

The library was this branch (`v0.0.31-flexwip.q78b`). These runs use the Flex worker default from #643: no worker options were set, and the grant was 32. Each query ran in a fresh process with full fetch and validation against the SF1 DuckDB source.

**Q8:**

| Build | Seconds | Rows read | Redis commands | Plan |
|---|---|---|---|---|
| Demand limit only (earlier build) | 136.9 | 1,705,175 | 1,718,682 | serial candidate fact; orders branch full parallel read |
| This change, single run | **17.4** | 542,390 | 550,127 | shared candidates: lineitem in 171 chunks (about 44k rows); the row total is consistent with the orders and customer filters also reading candidates |
| This change, warm-up then 3 timed runs (60 s deadline) | 22.2, then **31.4 / 38.2 / 30.5** | — | — | same |

All runs validated (2 rows). #643's build is estimated to read lineitem and orders whole, about 7.7M rows. It wasn't timed at SF1 for Q8; the coordinating session's earlier runs of main timed out at 60 s.

**Q7:**

| Run | Seconds | Rows read | Round-trip worker-seconds |
|---|---|---|---|
| First run (300 s deadline) | **31.1** | 1,658,513 | 396 |
| Next process (60 s deadline) | timeout | — | — |
| Two runs in one process (300 s deadline) | 82.6, 61.2 | 1,658,513 | 1,070 (last run) |

Every completed run validated (4 rows).
- **Lineitem:** now about 146k shared candidates in 570 chunks, instead of 6M rows.
- **Orders branch:** Q7 has no orders filter, so the branch still reads all 1.5M orders.
- **Timing:** the time follows how much of orders the 8 GiB RAM tier holds. **Q7 is not reliably under 60 s on this deployment.**

## Not established

- **Q7's orders read:** narrowing it would need its customer key demand (about 12k keys at SF1) pushed into orders. At #631's per-term Search cost, a 12k-term union is unlikely to pay off, so it was not attempted.
- **Overlays after DML:** they keep their existing plan (filters not pushed to Search under Flex), so the shared path was exercised on Redis only for single-segment tables.
- **Other SF1 queries:** Q3, Q5 and Q9 were not rerun at SF1. Their lineitem filters exceed the shared budget or are absent, so their dominant reads are unchanged.
- **No full harness pass.** Only the runs above.
- **Review:** human review is pending.
