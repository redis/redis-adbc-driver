# Validation

2026-11-24; checks ran on `8bb53c6560bc2bf3e2e802a531002500a7db2f54` plus this change.
The commit's base `e7dc1f5b` adds only documentation (#628). See [intent](intent.md),
[spec](spec.md), [plan](plan.md).

Environment: isolated Compose project on Redis Software 8.8.0, one shard, active
verified QPF scaling factor 4, `mnp`, 32 connections, `noeviction`, TLS, 8 GiB
database. macOS ARM64 host client through a Docker published port. Timings are
relative evidence, not the issue's Linux-namespace SF1 profile.

## Tests

- Failing-before: `TestSQLSemiJoinResiduals` on the #630 base (`89e22c7d`, with only
  `semijoin.go` / `query.go` reverted) failed every work-count assertion. Every
  residual shape ran per row with 0 semi-joins (Q21 shape: 19 per-row runs).
  All result assertions passed, and the per-row oracle agreed.
- After: `TestSQLSemiJoinResiduals` passes, including a scanned and a Q21-shaped joined
  outer side over a 2,402-row inner side, each probed in more than one 256-key
  batch with no per-row runs.
- `TestSQLAliasScopeSubqueryPaths` passes with its updated expectation
  (`x.k = e.k AND x.v > e.v` now decorrelates; `x.v > e.v + 0` still runs 5 times).
- Every integration test that uses EXISTS or subquery work counts, plus the
  #630 selection (`TestSQLSemiJoins`, `TestSQLSubqueries`, `TestSQLScalarSubqueryRows`,
  `TestSQLSubqueryDML`, `TestSQLInSubqueryHashSet`, `TestSQLGroupedSubquery*`,
  `TestSQLDistinctOnInCorrelatedSubquery`, `TestSQLSubqueryOuterOnlyCondition`,
  `TestSQLAliasScope*`, `TestSQLEmptySetOpsAndSubqueries`, `TestSQLJoins`, joins,
  lateral, row-value, quantified and set-operation suites): 65 top-level tests
  run, 63 pass, 2 skip. `TestSnapshotLookupJoinUsesVisibleCandidates` and
  `TestSnapshotDeltaPredicatesAggregatesAndRepeatedChanges` need `REDIS_SNAPSHOT_URI`
  for a disposable database and were not run on the shared fixture.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: exit 0; integration tests skip.
- `gofmt -l` on the changed files: clean.

## Subset measurement

10% TPC-H subset; fresh-process runs with a full fetch, validated against DuckDB
2.0.1 (row multiset, rel 1e-7, abs 1e-6). "Rows" counts every row the driver
processed, including probe and subquery rows.

| Q21 | result | rows | commands | read | allocated |
|---|---|---:|---:|---:|---:|
| main `c27bb970` (parent baseline) | timeout at 120 s | 119,296 | 404,988 | 1,199 MB | 8,198 MB |
| #630 `89e22c7d` | timeout at 120 s | 104,803 | 190,882 | 378 MB | 2,752 MB |
| this change, pre-merge | 12.64 s, valid | 171,473 | 172,048 | 38.3 MB | 725 MB |
| this change on `8bb53c65`, contended host | 15.5–23.8 s (6 runs), valid | 171,473 | 172,040 | 38.3 MB | 725 MB |

Peak client RSS was 287–311 MiB. The last row ran while other sessions loaded
other fixtures on the same Docker VM (about 160% and 120% container CPU). A Q4
control in the same window took 4.1–6.5 s against 2.56 s in #630's record, and
Q21's work counts did not change.

Breakdown (temporary instrumentation, pre-merge, not committed): `runJoin`
materialized 7,676 joined rows in 0.99 s. 58 probe batches of 256 keys read
62,105 inner rows in 6.9 s of 8.0 s. One 256-key probe issues five keyset
`FT.SEARCH` pages of 256 candidates, each followed by a 256-row, four-field HMGET
pipeline: about 88 ms of about 113 ms. `FT.PROFILE` of the union aggregate itself
reported about 11 ms. On this route, 256-key probes over 2,048 keys took about
885 ms serially and 473 / 227 / 182 ms on 2 / 4 / 8 concurrent connections.

SF1 has about ten times Q21's outer keys and is not measured here. Do not close
#587 before SF1 Q21 validates under 60 s on the issue profile.
