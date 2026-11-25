# Validation

2026-11-24; base `fdd49f7c`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Environment: isolated Redis Software 8.8.0 fixture (one shard, verified active
4x QPF, `mnp`, 32 connections, TLS) holding the 10% TPC-H subset (every order
with `o_orderkey % 10 = 1` and its lineitems, full dimension tables). Flex mode
is the driver option `adbc.redis.flex=true` against that RAM database. That
reproduces the driver-side Flex read path (exact row-ID directories, no Search
candidates), not flash latency.

## Tests

- `TestSQLFlexSemiJoinBuildsInnerOnce` (new; 2,000-row inner side, 2,500
  distinct outer keys) fails on unmodified main. EXISTS, NOT EXISTS and
  correlated IN each ran 10 page-probe batches (`{runs:0 semiJoins:10}`), every
  one a full inner scan under Flex. With the change, each runs exactly one
  whole build (`semiJoins:1`, no per-row runs). Results, including NOT IN
  three-valued logic and NULL keys, were already correct and are unchanged.
- Integration selection `SemiJoin|TestSQLSubquer|TestSQLScalarSubquery|
  TestSQLInSubquery|TestSQLGroupedSubquery|TestSQLDistinctOn|
  TestSQLEmptySetOpsAndSubqueries|TestSQLAliasScope|TestSemiKey|Exists|Correlat|
  Flex|Stopgap|TestDeferred`: `ok`, 128 passed, 0 failed. The 60 skips include
  tests that need a real Flex database (`REDIS_FLEX_URI`), which was not
  available for testing.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages ok (integration
  tests skip there).

## TPC-H Q22 (10% subset)

Linux ARM64 client in the fixture's network namespace (4 CPU / 8 GiB), fully
fetched and validated against DuckDB 2.0.1 (row multisets, rel 1e-7 / abs 1e-6).

| Q22, direct driver | result | rows read | Redis commands | bytes read |
|---|---|---:|---:|---:|
| main `fdd49f7c`, default mode | 13.98 s, valid | 272,242 | 275,311 | 67 MB |
| main `fdd49f7c`, Flex mode | timeout at 300 s | 63,708,288 | 63,957,211 | 3.6 GB |
| this change, Flex mode | 9.78 / 5.98 / 6.19 s, valid | 450,000 | 451,240 | 34 MB |

Not shown: SF1 on a real Software Flex database (busy with a separate
benchmark when this was measured). Other Flex read costs (#636) are unchanged.
