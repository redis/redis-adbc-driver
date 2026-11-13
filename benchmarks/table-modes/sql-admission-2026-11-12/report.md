# Managed SQL admission fix benchmark

Measured November 12, 2026 on **unreleased fix source**
`8e178ba17f8775992ab77fc5710d1375528cd4da`, built as
`v0.0.26-dev.sql-admission` with GetInfo verification. These are new observations;
[earlier v0.0.26](../2026-11-12/report.md) and
[post-Arrow](../post-arrow-2026-11-12/report.md) conflicts remain historical evidence.

## Existing-table writes

Median seconds for 10,000 rows × 105 public columns plus an ordinal. SQL INSERT
and Arrow append use up to 1,000 rows per call. Three measured trials after one
warmup, alternating forward/reverse case order. All 12 trials completed and
passed independent value/type/multiplicity checks. **No retries or conflicts.**

| Path | Writers | Median seconds | Min–max seconds |
|---|---:|---:|---:|
| Managed SQL | 1 | 8.478 | 8.468–8.582 |
| Managed SQL | 4 | 19.449 | 18.712–20.055 |
| Managed ARROW | 4 | 18.733 | 10.669–19.553 |

Four writer processes still submit concurrently; same-table managed SQL DML now
waits at the shared leased admission gate before executing. Arrow append uses
the same gate. Each admitted call evaluates current state and publishes its own
statement snapshot. This resolves the observed competing SQL workloads without
replaying input or weakening publication validation; it does not imply parallel
same-table execution or linear write scaling.

## Synchronized correctness control

The unchanged `concurrency_control.py` also runs four workers, each writing 25
rows × 105 columns, synchronized after connections and input preparation.
Three trials per API: **all four SQL writers and all four Arrow writers succeed
in every trial**, with all 100 visible rows independently checked. In the earlier
v0.0.25/v0.0.26 controls, SQL had one successful writer and three conflicts while
Arrow had four successes. These controls establish correctness, not throughput.

## Timing, scope and environment

Existing table/index preparation and full output validation are outside timing.
The clock includes fresh worker startup/imports, loading/converting source data,
SQL literal construction, connections, complete writes and shutdown. Managed
publication includes private index readiness. No incomplete workload contributes
a successful latency. No reads, unmanaged/native comparison, creation, full-scale
loading, 100M ClickBench, physical Flex or Flight SQL rerun is claimed here.

DDL, additional targets in data-changing CTEs, changed managed read dependencies
and older clients can still conflict. The gate has no fairness guarantee. These
are within-run workload observations on a shared workstation, not confidence
intervals or an isolated driver-version speedup.

Redis Software 8.2.6-25 / Redis 8.8.0 / Search 8.8.8; one RAM shard, 4 GiB
database, noeviction. Effective active 4× QPF, six Search workers, mnp routing and
32 connections verified before every case and at completion. Certificate-verified
TLS and RESP3. Container: four CPUs/eight GiB; host: Apple M5 Max arm64, 18 logical
CPUs/36 GiB. Go 1.27.3; Python 3.15.2, PyArrow 26.0.1, ADBC manager 1.13.0,
redis-py 8.1.0 and hiredis 3.5.0. All live tests finished before timing.

Source Arrow SHA-256: `a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2`.
Binary SHA-256: `9422d37d20ae402342e44cb331169f4731e85a6b7c8154e4b2c4ef107184bf35`.

## Reproduction and evidence

Build the pinned source using `make -C go build adbc-cli
VERSION=v0.0.26-dev.sql-admission`. Use the fixture/environment and fresh
private paths described in the [harness README](../README.md). Instantiate
`arrow_compare.Benchmark` with 10K write rows, 1K batch size, three measured
write repetitions and the source identified above; select these existing cases:

```python
b.write_cases('writes', [('managed', 'sql', 1, 'append'),
                         ('managed', 'sql', 4, 'append'),
                         ('managed', 'arrow', 4, 'append')])
```

Run `concurrency_control.py` in a separate invocation on the reset task-owned
fixture, with the same source/library/version/source commit. The typed source,
credentials, infrastructure identifiers and raw logs remain private.

[measurements.json](measurements.json) retains all timed trials, including warmups;
[concurrency-controls.json](concurrency-controls.json) retains synchronized outcomes;
[summary.json](summary.json) retains recomputed distributions. Run
`python3 benchmarks/table-modes/sql_admission_audit.py` for coverage, completion,
configuration and arithmetic checks. See the
[validation record](../../../docs/sdlc/changes/managed-sql-admission/validation.md)
for tests, skips, review limits and cleanup.
