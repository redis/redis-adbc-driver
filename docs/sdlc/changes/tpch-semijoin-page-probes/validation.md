# Validation

2026-11-24; base `c27bb970d938d25b82e427dcfb2efe59fd40d7a9`. See [intent](intent.md),
[spec](spec.md), [plan](plan.md).

Environment: isolated Compose project on Redis Software 8.8.0, one shard, active
verified QPF scaling factor 4, `mnp`, 32 connections, `noeviction`, TLS, 8 GiB
database. macOS ARM64 host client through a Docker published port. Timings are
relative evidence, not the issue's Linux-namespace SF1 profile.

## Tests

- `TestSQLSemiJoins`: every result assertion passes unchanged, including the
  failing-inner-row (`10 / i.z`) per-row fallback. The two work-count assertions
  that encoded the old large-inner strategy (20 per-row runs, then one build;
  5 per-row runs) now expect one page probe and no per-row runs. New case:
  2,500 outer keys (several 256-key probes), NULL keys and keys without inner
  rows, with EXISTS, NOT EXISTS and correlated IN counts checked.
- Subquery selection (`TestSQLSubqueries`, `TestSQLScalarSubqueryRows`,
  `TestSQLSubqueryDML`, `TestSQLInSubqueryHashSet`, `TestSQLGroupedSubquery*`,
  `TestSQLDistinctOnInCorrelatedSubquery`, `TestSQLSubqueryOuterOnlyCondition`,
  `TestSQLAliasScopeSubqueryPaths`, `TestSQLEmptySetOpsAndSubqueries`,
  `TestSemiKeyMatchesCompareValues`, `TestDeferredComposite…`): pass.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: exit 0. Integration tests skip
  there.

## Subset measurement

10% TPC-H subset; one fresh-process run with a full fetch, validated against
DuckDB 2.0.1 (row multiset, rel 1e-7, abs 1e-6).

| Q4 | result | outer rows | commands | read |
|---|---|---:|---:|---:|
| main | timeout at 120 s | 24,815 processed | 363,885 | 1,388 MB |
| 1,000-key probes | 3.84 s, valid | 28,497 | 28,651 | 5.7 MB |
| 256-key probes | 2.56 s, valid | 28,497 | 28,711 | 5.7 MB |

Probe batch timing for `SELECT l_orderkey FROM lineitem WHERE l_orderkey IN
(…)`: 100 keys / 399 rows in 44 ms, 250 / 1,027 in 103 ms, 500 / 2,029 in 240 ms,
1,000 / 3,982 in 604 ms. A 4,040-row key range took 65 ms, so the union is
re-evaluated per result page. That union cost is tracked separately. SF1 has
about ten times the outer keys and is not yet measured; do not close #580 before
SF1 runs on the issue profile validate.
