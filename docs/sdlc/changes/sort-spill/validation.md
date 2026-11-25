# Validation

2026-11-25; base `9ea7ba76` (main). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Environment: isolated Compose project on Redis Software 8.8.0, one shard, active verified QPF
scaling factor 4, `noeviction`, TLS, 4.5 GiB database (`software-setup.sh`, `SOFTWARE_SHARDS=1`).
Client: macOS ARM64 host (10 CPUs, Go 1.27.4) through a Docker published port; other sessions'
fixtures shared the Docker VM.

## Tests

- `TestSQLSortSpill` (new): 3,000 rows with tied, NULL, NaN / ±Infinity, decimal, timestamp and
  non-ASCII string values, through the ADBC streaming execute path.
  - Each of six ORDER BY queries runs at the default budget, where it doesn't spill, and with
    `testSortSpillBytes` = 16 KiB, where every one spills at least two runs. The two results
    are compared row for row, in order. The queries cover DESC, `NULLS FIRST/LAST`, a residual,
    select-list expressions, and `LIMIT 2000 OFFSET 17`.
  - At least one sort merged in more than one pass (more than 32 runs).
  - Every spilled buffer was at most the budget plus one row's estimate.
  - Spill directories are gone after each query, and after a stream released after its first
    64-row batch.
  - `1 / (k - 96)` in the select list does not raise when the `k = 96` rows are skipped by
    OFFSET or fall beyond the LIMIT, matching the in-memory path, which evaluates the select
    list only for returned rows.
- Unit-only `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages pass. `gofmt -l` on the
  changed files: clean.
- Integration selection `-run 'Order|Sort|Stream|Union|Limit|Offset|TopK|Select|Window|Distinct|Collat|NaN|Null'`
  (227 top-level tests) through `software-setup.sh --exec`: `ok … 1232.530s`. After a final edit
  (the scan is closed once, by its deferred close), `TestSQLSortSpill` and `TestUnionAllStream*`
  were rerun: pass.

The UNION ALL stream tests read `streamReader.union` through a type assertion now that it is an
interface; their assertions are unchanged.

## Peak memory on the subset

Fixture: the 10% TPC-H subset `lineitem` (600,093 rows) loaded through the driver. Query:
`SELECT l_orderkey, l_linenumber, l_comment, l_extendedprice FROM lineitem ORDER BY
l_comment || '', l_orderkey, l_linenumber`. The ORDER BY is an expression, so Search cannot sort
it. Each run is a fresh Python process (ADBC driver manager 1.13.0, PyArrow 26.0.1) that fetches
the whole stream. The result's exact order is compared with DuckDB 2.0.1's
`ORDER BY l_comment COLLATE C, l_orderkey, l_linenumber`. Peak RSS is the process's `ru_maxrss`.

| build | run | rows | order matches DuckDB | peak RSS | time | phase |
|---|---|---:|---|---:|---:|---|
| main `9ea7ba76` | 1 | 600,093 | yes | 1,667 MB | 24.55 s | `select_scan` |
| this change | 1 | 600,093 | yes | 549 MB | 33.79 s | `external_sort` |
| main `9ea7ba76` | 2 | 600,093 | yes | 1,399 MB | 35.78 s | `select_scan` |
| this change | 2 | 600,093 | yes | 556 MB | 27.03 s | `external_sort` |

The integration selection ran on the same fixture during these runs, so times are noisy; peak
RSS fell by about two thirds. Allocation during execute fell from 3.65 to 2.9 GB.

## Not established

- The join half of #654: snowflake branches above `snowflakeBranchRows` still fall back to the
  eager join, and the eager join still materializes intermediate rows. That needs a partitioned
  (Grace-style) hash join, which is not part of this change.
- Sorts in subqueries, CTEs, joins, set operations, windows and DISTINCT, and ORDER BY or
  select-list expressions with subqueries, aggregates or volatile functions, keep the in-memory
  path.
- The budget is an internal constant (256 MiB, estimated), with no user option.
- SF1 and multi-shard behavior were not measured locally.
- Human review is pending.
