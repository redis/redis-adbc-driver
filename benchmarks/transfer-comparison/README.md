# Historical transfer benchmarks

Published October 22, 2026 from previously local October 21 evidence. These
measurements predate the later driver optimizations. For the newer RESP3
comparison of first-batch latency and complete transfer, see the
[incremental-read results](../incremental-read/2026-10-22/README.md).

All experiments here use a 100,000-row, 105-column ClickBench prefix. They do
not measure a 100-million-row transfer or an official ClickBench query score.

| Evidence | Recorded executions | Purpose |
|---|---:|---|
| [Cross-product report](2026-10-21/report.md) and [raw results](2026-10-21/results.json) | 32 | Redis ADBC, PostgreSQL ADBC, ClickHouse HTTP Arrow and embedded DuckDB |
| [Native Arrow results](2026-10-21/native-arrow/results.json) | 16 | Intermediate redis-py comparison with Arrow output |
| [Native output results](2026-10-21/native-output/results.json) | 24 | ADBC versus redis-py with Arrow and Python bytes/pickle output |
| [#396 investigation](2026-10-21/issue396/report.md) and [sweep](2026-10-21/issue396/sweep-results.json) | 32 | Stream batch-size sweep and source-identified ADBC/native comparison |
| [Native discovery results](2026-10-21/issue396/discovery-results.json) | 16 | Known keys versus Search discovery plus HMGET |
| [Stage profiles](2026-10-21/issue396/stage-results.json) | 8 | Diagnostic timing, allocation and incoming-byte breakdown |

The five end-to-end matrices record exact full-value/multiplicity validation
for all 120 executions, including 30 warmups and 90 measured runs. The stage
experiment records row counts for eight executions and exact validation of
only the final output from each of its two strategies. These are retained
historical validation records; publication does not rerun the measurements.

## Provenance and interpretation

- The original cross-product and native comparisons pin the Redis library by
  SHA-256, but its source revision was not independently attested. The #396
  investigation identifies source commit `20de26967b9974fa344f15de2b71491039321d86`.
- Native redis-py and the stage profiler explicitly used RESP2. This archive
  preserves those settings; the current driver defaults to RESP3. Do not
  combine these numbers with newer runs as if they were one matched matrix.
- The original reports' recommendations and issue-state statements describe
  the investigation at the time. They do not describe today's open work.
- CPU profiles, allocation profiles, logs, raw trials and measured Python
  snapshots are preserved. [Archive identities](archive-manifest.json) record
  original file hashes, publication hashes and the few documentation changes.
- The Go stage profiler was moved from the working tree's
  `go/wide_transfer_profile_test.go` to
  [the #396 evidence directory](2026-10-21/issue396/wide_transfer_profile_test.go)
  without changing its bytes. It depends on historical private driver helpers
  and intentionally remains outside the current Go test suite. The #396
  reproduction instructions copy it into the recorded source revision.

## Reproduction

Follow the corresponding report and manifest for the fixture hash, packages,
source/binary identities and dedicated server configuration. Source parquet,
Arrow outputs and compiled libraries are not included. The local credentials
in the scripts are throwaway fixture credentials; the loaders replace fixture
tables and require dedicated servers.

`benchmark.py` is the extended harness in its original repository location.
Dated Python snapshots retain their original relative-import assumptions. To
run a snapshot from its archived location, provide the ClickBench helper path:

```sh
PYTHONPATH="$PWD/benchmarks/clickbench${PYTHONPATH:+:$PYTHONPATH}" \
  python benchmarks/transfer-comparison/2026-10-21/benchmark-original.py --help
```

For #396, also set `REDIS_TRANSFER_REPO_ROOT` as shown in its report. Copy the
scripts to a fresh work directory before running the sweep/discovery commands;
they write results beside themselves. A rerun with a different driver, host or
protocol produces new measurements rather than validating the old timings.
