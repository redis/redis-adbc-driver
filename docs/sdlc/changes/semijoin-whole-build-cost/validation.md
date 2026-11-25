# Validation

2026-11-25; base `ed196803` (main, including #673's complete union reads). See [intent](intent.md),
[spec](spec.md), [plan](plan.md).

Environment: isolated Compose project on Redis Software 8.8.0, one shard, active verified QPF
scaling factor 4, `noeviction`, TLS, 4.5 GiB database (`software-setup.sh`, `SOFTWARE_SHARDS=1`).
Client: macOS ARM64 host (10 CPUs) through a Docker published port. Two other sessions' Software
fixtures shared the Docker VM. Fixture: the 10% TPC-H subset (orders with `o_orderkey % 10 = 1`
plus their lineitems, full dimension tables; 1.91M rows) loaded through the driver.

## Cost constants

Through the driver on main `2ed3b3df`, fully fetched. The outer side is
`lineitem l1 JOIN orders o` with `o_orderdate < 1992-01-01 + m months AND o_orderstatus = 'F'`.
The semi-join is `EXISTS (SELECT * FROM lineitem l2 WHERE l2.l_orderkey = l1.l_orderkey AND
l2.l_suppkey <> l1.l_suppkey)`. Probe time is the query's time minus the same query without the
EXISTS. Every result matched DuckDB.

| workers | months | distinct outer keys | with EXISTS | without | probes | per key |
|---:|---:|---:|---:|---:|---:|---:|
| 1 | 3 | 5,623 | 6.21 s | 4.71 s | 1.50 s | 0.267 ms |
| 1 | 12 | 23,005 | 12.94 s | 5.21 s | 7.73 s | 0.336 ms |
| 1 | 42 | 73,252 | 31.83 s | 5.98 s | 25.85 s | 0.353 ms |
| 4 | 3 | 5,623 | 4.21 s | 1.82 s | 2.39 s | 0.425 ms |
| 4 | 12 | 23,005 | 6.12 s | 2.72 s | 3.40 s | 0.148 ms |
| 4 | 42 | 73,252 | 9.34 s | 3.15 s | 6.19 s | 0.085 ms |
| 8 | 3 | 5,623 | 2.22 s | 1.25 s | 0.97 s | 0.173 ms |
| 8 | 12 | 23,005 | 3.71 s | 1.72 s | 1.99 s | 0.086 ms |
| 8 | 42 | 73,252 | 7.00 s | 2.11 s | 4.90 s | 0.067 ms |

The whole body (`SELECT l_orderkey, l_suppkey FROM lineitem`, 600,093 rows) read in 5.17 /
5.14 / 5.45 s (8.6–9.1 µs per row) at every worker setting.

The whole builds below took 6.5–6.8 s of semi-join time for the same 600,093 rows, about 11 µs
per row with grouping. So the constants are 11 µs per inner row and 350 µs per key, divided by
the workers. Before #631, serial probes cost 0.63–0.66 ms per key
(`tpch-semijoin-parallel-probes/validation.md`). The modelled crossover on this inner side is
about 18,900 keys serially, 75,000 with four workers and 151,000 with eight. Measured, eight
workers scale less than eight times, so the model errs towards probing.

## With the switch

Same queries, first build of this change (10 µs build constant), results validated against
DuckDB. "semi" is the query's time minus the same query without the EXISTS.

| workers | distinct keys | main | this change | path |
|---:|---:|---:|---:|---|
| 1 | 5,623 | 6.21 s (semi 1.50 s) | 6.14 s (semi 1.33 s) | probes |
| 1 | 23,005 | 12.94 s (semi 7.73 s) | 12.14 s (semi 6.81 s) | whole build |
| 1 | 73,252 | **31.83 s (semi 25.85 s)** | **11.70 s (semi 6.60 s)** | whole build |
| 4 | 5,623 / 23,005 | 4.21 / 6.12 s | 4.31 / 5.62 s | probes |
| 4 | 73,252 | 9.34 s (semi 6.19 s) | 9.45 s (semi 6.54 s) | whole build at 10 µs; probes at 11 µs (75,000-key threshold) |
| 8 | 5,623 / 23,005 / 73,252 | 2.22 / 3.71 / 7.00 s | 2.22 / 3.56 / 7.05 s | probes |

Command counts show the path: a whole build reads the 600,093 inner rows once (1,356,245 commands
at 73,252 keys against 1,050,623 for probes). The 4-worker case at 73,252 keys sits on the
crossover; the final 11 µs constant keeps it on probes.

## Tests

- `TestSQLSemiJoinWholeBuildByCost` (new), one worker, 3,000 inner lines (threshold 94 keys
  at 11 µs): 23 keys keep page probes and 376 keys build once, both with the expected count.
  With the switch off (an overlay adding `keepSemiProbes(t)`, i.e. main's behavior) it fails:
  `above the threshold (340 keys, threshold 85): 0 cost builds, {runs:0 semiJoins:2 inSets:2};
  want one whole build` (at the 10 µs constant).
- `TestSQLSemiJoinParallelProbes` and `TestSQLSemiJoinResiduals` exercise the probe mechanics
  on small inner sides that the switch would build once; they call `keepSemiProbes(t)`.
- `-race` selection `TestSQLSemiJoinWholeBuildByCost|TestSQLSemiJoinParallelProbes|TestSQLSemiJoins|TestSQLSemiJoinResiduals|TestSQLSubqueries|TestSQLSubqueryDML|TestSQLAliasScopeSubqueryPaths|SemiJoin|Exists|Subquer`
  (18 top-level tests) through `software-setup.sh --exec`: pass, no race reports. Rerun of the
  three semi-join tests with the final constant: pass.

- Unit-only `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages pass. `gofmt -l` on the changed
  files: clean.

## TPC-H Q4 / Q21 and the first-run outlier

Each query in four fresh processes per library, three in-process repetitions each, libraries
alternating, fully fetched and validated against DuckDB 2.0.1 (row multiset, rel 1e-7 /
abs 1e-6). All 48 runs validated. Q21's joined outer side has about 7,400 distinct keys per
subquery with 10 workers granted, far below the threshold, so both builds probe with the same
command counts. Q4's outer side is a scanned page, which the switch does not consider.

| query | library | runs | median | min / max | max ÷ median | first run in each process |
|---|---|---:|---:|---:|---:|---|
| Q4 | main `2ed3b3df` | 12 | 0.30 s | 0.28 / 0.38 s | 1.26 | 0.35 / 0.31 / 0.29 / 0.38 s |
| Q4 | this change | 12 | 0.31 s | 0.28 / 0.37 s | 1.17 | 0.34 / 0.36 / 0.30 / 0.37 s |
| Q21 | main `2ed3b3df` | 12 | 1.39 s | 1.29 / 1.90 s | 1.37 | 1.90 / 1.42 / 1.47 / 1.29 s |
| Q21 | this change | 12 | 1.46 s | 1.31 / 1.58 s | 1.08 | 1.48 / 1.50 / 1.53 / 1.47 s |

**Outlier.** The 70.9 s first repetition recorded for #633 did not recur. Across 24 Q21 runs in
eight fresh processes, no run exceeded 1.4× its median, and first runs were within the
spread of later ones. Earlier today, before and with #631, Q21's subset medians were 2.99 and
2.50 s; the lower 1.4 s here also reflects less concurrent load on the shared Docker VM.
The original series ran while another session's SF1 Flex load used 133–393% container CPU on
the same VM (`tpch-semijoin-parallel-probes/validation.md`). That contention, not a driver path,
is the likely cause, but it was not reproduced, so it remains undiagnosed rather than explained.

## Not established

- SF1 latency on the issue profile; SF1 cannot be hosted on this machine (#648).
- Scanned outer sides keep probing even with many distinct keys; their keys are unknown before
  probing.
- Multi-shard costs were not measured; the constants come from one Software shard.
- Human review is pending.
