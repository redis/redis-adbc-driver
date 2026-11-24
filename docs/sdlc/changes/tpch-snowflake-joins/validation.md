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

Fixture: 10% of the retained SF1 source (every order with `o_orderkey % 10 = 1` plus its lineitems, full dimension tables), 1.91M rows. Run from the macOS host through the Docker published port, on a host shared with other sessions' fixtures and workloads. Each query runs in a fresh process and is fully fetched. Results are validated against DuckDB 2.0.1 as row multisets (rel 1e-7 / abs 1e-6).

| Query | main `1245ac2c` (1 run) | this branch (3 runs, s) | Peak RSS main → branch |
|---|---|---|---|
| Q3 | 26.90 s | 5.11 / 5.39 / 3.46 | 743 MB → 292 MB |
| Q5 | 220.40 s | 4.21 / 3.67 / 3.64 | 6.40 GB → 417 MB |
| Q7 | 101.12 s | 2.19 / 2.11 / 2.13 | 8.66 GB → 365 MB |
| Q8 | process timeout (180 s) | 3.00 / 3.21 / 3.39 | — → 296 MB |
| Q9 | 40.92 s | 7.68 / 5.63 / 5.81 | 5.40 GB → 1.82 GB |
| Q18 (also affected) | 3.39 s | 1.75 / 2.56 / 2.55 | 458 MB → 529 MB |
| Q19 (also affected) | 3.56 s | 0.82 / 0.75 / 0.78 | 553 MB → 175 MB |
| Q21 (#632, also affected) | 9.86 s | 9.33 / 9.94 / 9.69 | 388 MB → 247 MB |

Every run validated. The main baseline ran once on the same contended host, and its Q3 is slower than its earlier uncontended 5.95 s, so the ratios are not controlled speedups.

Plans:
- Q3, Q5, Q8 and Q9 use the parallel fact reader.
- Q7's fact keeps the serial Search path, because its supplier-key union makes its filter selective.
- Q7's branches shrink to 798 suppliers and 12,117 orders through the implied nation lists.
- Q9 reads partsupp and orders with the parallel dimension reader.

## Not established

- SF1 latency and memory under the 60-second / 8 GiB budget. SF1 verification on Redis Software with verified 4x QPF, through the Linux namespace route, is pending in the coordinating session.
- At SF1, Q9's 6M-row fact scan dominates. Q7's fact uses a Search union of about 800 supplier keys.
- No full benchmark suite was run.
- Human review is pending.
