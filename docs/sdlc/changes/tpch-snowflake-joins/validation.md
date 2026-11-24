# Validation

2026-11-24; branch merged with `1245ac2c` (main including #629, #630, #628 and #632). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Failing-before regression

`TestSnowflakeQ5StreamsByDefault` uses only pre-existing APIs and default thresholds, with a 70,000-row in-memory fact. Copied onto unmodified main `c27bb970`, it fails: the Q5-shaped join ran the eager `lookup_join_order` plan. It passes with this change.

## Tests

Unit tests (`go/join_snowflake_test.go`):
- **Snowflake vs eager:** Q3/Q5/Q7/Q9-shaped and materialized joins run in both modes over a fixture with duplicate keys, NULL keys and unmatched rows. Row multisets and value types are identical in each case.
- **Planner:** the Q5 tree and residual, hub selection (a larger leaf and a referenced hub), cap fallback before any fact row, unsafe-shape rejections, the minimum fact size, predicate classes, implied literal disjunctions and the parallel-reader predicate gate.

The managed-table integration test (`TestSQLSnowflakeJoinsManaged`) runs the same comparison on Redis. It covers the parallel exact-directory fact reader, the broad-filter gate (q3), the parallel dimension reader (2,500-row orders) and the derived-table path.

`TestJoinCommonSafety` now pins the exact implied set for `(p_partkey=l_partkey AND p_size=1) OR p_size=2`: the whole `OR` plus `p_size=1 OR p_size=2`. The join equality absent from one branch is still not extracted.

Results:
- `gofmt` clean.
- Unit-only `env -u REDIS_URI go test -count=1 -p 1 ./...` passes on the merged tree (all 7 packages).
- On the shared Redis Software fixture (8.8.0, 1 shard, verified active 4x QPF, TLS), the integration selection `Star|Join|Q19|TPCDS|Deferred|Snowflake|Union|Natural|Lateral|SemiJoin|Subquer|Grouped|CTE|Common|FactDemand|OwnedFact|TestSQLPartitioned|Decimal|ScalarAggregate|WindowAggregates|QualifiedStar|DistinctOn|TagExactness` passes.
- That shared database intermittently refused writes with `OOM command not allowed when used memory > 'maxmemory'` while other sessions used it (about 3.4 GB data plus 1.9 GB Search indexes in 8 GiB). Every failure in those runs was that connect-time OOM. The 18 affected tests all passed when rerun.
- An earlier, broader selection that included partition and live-HASH suites hit the same OOM throughout. Those suites don't exercise this change and weren't rerun on a clean fixture.

## TPC-H subset

Fixture: 10% of the retained SF1 source (every order with `o_orderkey % 10 = 1` plus its lineitems, full dimension tables), 1.91M rows. Run from the macOS host through the Docker published port. Each query runs in a fresh process and is fully fetched. Results are validated against DuckDB 2.0.1 as row multisets (rel 1e-7 / abs 1e-6).

**Concurrent load:** the coordinating session's SF1 timed runs on a Software Flex server shared the host CPUs throughout. Times are labeled observations, not controlled speedups.

Both libraries were built side by side: `origin/main` `1245ac2c` and this branch `1f6a8f44`. Each query ran on main, then on the branch, back to back.

| Query | main `1245ac2c` (s) | branch (s) | Plan main → branch | Peak RSS main → branch |
|---|---|---|---|---|
| Q1 | 5.52 / 6.90 / 4.89 | 6.15 / 4.68 / 4.32 | unchanged (`partitioned_exact`) | 846 MB → 909 MB |
| Q3 (#579) | 13.95 / 8.32 / 9.89 | 9.86 / 9.36 / 4.65 | eager → snowflake, parallel fact | 767 MB → 302 MB |
| Q4 | 3.16 / 3.24 / 6.05 | 4.51 / 4.51 / 2.78 | unchanged (`select_scan`) | 117 MB → 111 MB |
| Q5 (#581) | 166.12 | 7.82 | eager → snowflake, parallel fact | 7.10 GB → 370 MB |
| Q7 (#582) | 148.28 | 5.07 | eager → snowflake | 6.61 GB → 368 MB |
| Q8 (#583) | process timeout (180 s) | 11.52 | eager → snowflake, parallel fact | — → 288 MB |
| Q9 (#584) | 50.08 / 41.62 / 41.06 | 12.98 / 12.56 / 8.67 | eager → snowflake, parallel fact | 4.64 GB → 1.79 GB |
| Q18 | 3.81 / 2.84 / 2.70 | 3.61 / 3.07 / 2.58 | outer join eager → snowflake | 537 MB → 502 MB |
| Q19 | 3.75 / 3.85 / 2.65 | 0.65 / 0.60 / 0.67 | eager join with implied lists | 530 MB → 162 MB |
| Q21 | 11.28 / 11.20 / 12.18 | 11.78 / 11.98 / 12.75 | joined rows now streamed | 341 MB → 213 MB |

Every run validated. Q5, Q7 and Q8 ran once per side, because main takes minutes on them. Earlier same-day runs of this branch, three each, were Q5 3.64–4.21 s, Q7 2.11–2.19 s and Q8 3.00–3.39 s.

Q4, Q18 and Q21 were then rerun as five alternating pairs, with the first side alternating:

| Query | main (s) | branch (s) | Medians |
|---|---|---|---|
| Q4 | 3.67 / 2.26 / 2.58 / 3.06 / 2.80 | 2.64 / 2.45 / 2.83 / 3.08 / 3.10 | 2.80 vs 2.83 |
| Q18 | 7.49 / 4.29 / 2.86 / 3.63 / 3.06 | 7.16 / 5.83 / 4.24 / 3.19 / 2.10 | 3.63 vs 4.24 |
| Q21 | 9.02 / 9.82 / 9.70 / 10.51 / 11.27 | 9.12 / 9.65 / 9.72 / 9.99 / 13.04 | 9.82 vs 9.72 |

All 30 runs validated.
- **Q4 and Q21:** no difference.
- **Q18:** pairwise branch − main differences are −0.33, +1.54, +1.38, −0.44 and −0.96 s (median −0.33 s), while load drifted from about 7 s down to about 2 s. That is no consistent slowdown. Q18's cost is its inner grouped aggregate (#629), which is the same on both sides.
- **Q1:** its plan is unchanged.

## Plan changes outside Q3/Q5/Q7/Q8/Q9

- **Q19:** the implied per-column literal lists push `p_brand IN (3 values)` and `p_container IN (12 values)` into the part scan. The complete `OR` and the #586 common conjuncts remain, and Q19 still uses the eager join. `TestJoinCommonQ19Plan`, `TestJoinCommonResults` and `TestSQLJoinCommonQ19` pass.
- **Q18:** its outer customer/orders/lineitem join is now a snowflake. The orders branch carries the uncorrelated `IN` subquery as a pushed filter, so it stays on the serial readers. DATE `GROUP BY` (`o_orderdate`) is admitted to streaming.
- **Q21:** its lineitem/orders/supplier/nation join now streams in `scanMem`. #632's page probes then filter the returned rows unchanged.
- **Other non-aggregate inner joins** whose fact has at least 65,536 rows also stream, so their unordered row order may differ.

## Not established

- SF1 latency and memory under the 60-second / 8 GiB budget. SF1 verification on Redis Software with verified 4x QPF, through the Linux namespace route, is pending in the coordinating session.
- At SF1, Q9's 6M-row fact scan dominates. Q7's fact uses a Search union of about 800 supplier keys.
- No full benchmark suite was run.
- Human review is pending.
