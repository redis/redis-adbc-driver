# Validation

2026-11-24; base `1245ac2cdd944c68324f48dac7084b9b8c49b271`. See [intent](intent.md),
[spec](spec.md), [plan](plan.md).

Environment: isolated Compose project on Redis Software 8.8.0, one shard, active
verified QPF scaling factor 4, `mnp`, 32 connections, `noeviction`, TLS, 8 GiB
database. macOS ARM64 host client through a Docker published port. During every
measurement, another session's SF1 Flex load ran on the same Docker VM
(`redis-adbc-flex` at 133–393% container CPU). Timings are relative evidence,
not the issue's Linux-namespace SF1 profile.

## Tests

- `TestSQLSemiJoinParallelProbes` (new) passes, also under `-race`. Serial
  (`query_worker_budget=1`) and parallel (`4`, `query_workers=4`) databases agree
  on joined and scanned outer sides with probes only, and only the parallel one
  runs parallel pages. A `10 / 0` in one batch's group reports `division by zero`
  in both modes. Its first version cancelled sibling workers through a derived
  context. That interrupted the execution's sockets, and the per-row fallback
  then failed with `use of closed network connection`; workers now stop with a
  flag.
- With `-race`: `TestSQLSemiJoins`, `TestSQLSemiJoinResiduals`,
  `TestSQLSemiJoinParallelProbes`, `TestSQLSubqueries`, `TestSQLSubqueryDML` and
  `TestSQLAliasScopeSubqueryPaths` pass with no race reports. The default
  harness budget (auto, CPU-sized) runs their probes in parallel.
- The subquery selection of #632 plus the new test (66 top-level tests): 64
  pass, 2 skip. `TestSnapshotLookupJoinUsesVisibleCandidates` and
  `TestSnapshotDeltaPredicatesAggregatesAndRepeatedChanges` need
  `REDIS_SNAPSHOT_URI` for a disposable database.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: exit 0; `gofmt -l`: clean.

## Subset measurement

10% TPC-H subset; full fetch, validated against DuckDB 2.0.1 (row multiset,
rel 1e-7, abs 1e-6). Work counts are identical in both modes (Q21: 171,473 rows,
172,040 commands, 38.3 MB read; Q4: 28,497 rows, 28,703 commands, 5.7 MB).

| | serial (`query_workers=1`) | parallel (default budget) |
|---|---|---|
| Q21 | 10.1, 10.8 s | 1.8–5.7 s in 16 runs, one outlier at 70.9 s |
| Q4 | 2.8, 4.7 s | 0.36–0.57 s in 8 runs, plus 1.35 s on one first in-process run |

The 70.9 s Q21 run was the first repetition of one in-process series. It did
not recur in 16 later runs (fresh processes and in-process series). Its cause
was not identified; the work counts matched the other runs. The final build (with
panic re-raise) gave Q21 2.62 / 2.32 / 2.34 s and Q4 0.57 / 0.38 / 0.44 s.

## Probe and whole-build cost (crossover)

Through the driver on the same host, fully fetched:

| | time |
|---|---|
| whole EXISTS body (`l_orderkey, l_suppkey`, 600,093 rows) | 5.5–6.7 s (9–11 µs/row) |
| whole NOT EXISTS body (+ `l_receiptdate > l_commitdate`, 379,452 rows) | 15.5–21.5 s |
| serial probes, 128 / 256 / 512 keys | 0.63 / 0.66 / 0.80 ms per key |
| 4 / 8 concurrent 256-key probes | 0.17 / 0.08 ms per key |

A whole build beats probing only when a statement probes more distinct keys than
about build time / per-key cost. For the cheaper body that is about 9,000 keys
serially, 35,000 with four workers and 73,000 with eight: about 1.5%, 6% and 12%
of the 600,093 inner rows, or 6%, 24% and 49% of the 150,000 orders. The filtered
NOT EXISTS body's threshold is about three times higher. The subset's Q21 probes
about 7,400 keys per subquery and SF1 about ten times that against ten times the
inner rows, so probes stay ahead. A whole build also runs as one serial scan and
retains every inner row's key and residual values. 256-key batches stay: 128 was
within noise and 512 was slower.

SF1 Q21 / Q4 have not been measured with this change; do not close #587 or #580
before they validate under 60 s on the issue profile.
