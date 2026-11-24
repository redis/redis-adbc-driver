# Validation

2026-11-24; base `c27bb970d938d25b82e427dcfb2efe59fd40d7a9`. See [intent](intent.md),
[spec](spec.md), [plan](plan.md).

## Environment

Isolated Compose project on Redis Software 8.8.0, one shard, active verified
`query_performance_factor` scaling factor 4, `mnp` routing, 32 connections,
`noeviction`, TLS, ACL user `default`, 8 GiB database, persistence disabled.
macOS ARM64 host client through a Docker published port. This route is
known to delay large replies, and it is neither the issues' Linux namespace
client nor SF1, so timings are relative evidence only.

## Tests

- `TestSQLPartitionedDecimalSums` with the original `partitioned_aggregate.go`
  fails in all four admitted shapes ("exact decimal sums were not admitted to
  partitioned aggregation"). With the change, all pass: partition results equal
  `serialDriverAggregate` including value type, precision and scale; rejected
  shapes (decimal division, decimal MIN beside a decimal SUM, COUNT DISTINCT,
  no GROUP BY) stay rejected.
- `TestSQLPartitionedAggregateExact`, `TestSQLPartitionedGroupCapRecovery`,
  `TestSQLPartitionedWorkersTopKAndCleanup` pass.
- `-run 'TestSQLPartitioned|Aggregate|TestSQLClickBench|…Subquer…|Exists'`
  integration selection: `ok`.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: exit 0. Integration tests
  skip there, so that pass does not show Redis compatibility.

## Subset measurement

10% subset: 150,000 orders and 600,093 lineitems, with full customer/part/partsupp/
supplier/nation/region. Each query ran once in a fresh process with a full fetch,
validated against DuckDB 2.0.1 as a row multiset (rel 1e-7, abs 1e-6).

| Query | main | change | plan |
|---|---:|---:|---|
| Q1 | 10.05 s (serial, 591,805 commands) | 2.56 s, valid | `partitioned_exact`, partial groups |
| Q18 | 5.07 s | 1.60 s, valid | inner GROUP BY partitioned |

Q1 peak RSS rose from 127 MB to 890 MB, from per-worker spill buffers;
allocation 6.6 GB vs 3.1 GB. SF1 completion under the 60 s deadline is not yet
shown. Do not close #578/#585 until SF1 runs on the issue profile validate.
