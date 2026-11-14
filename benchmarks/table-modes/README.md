# Managed/unmanaged table comparison

[November 12 v0.0.26 results](2026-11-12/report.md) compare managed SQL,
unmanaged SQL and redis-py+hiredis on Redis Software with verified active 4x QPF.
This directory changes benchmark tooling/evidence, not production driver behavior.
The raw Redis client is redis-py, not the managed snapshot-aware `NativeClient`.

[Scenario chart data](scenario-charts-2026-11-13/report.md) adds point reads,
INSERT/UPDATE/DELETE, filtered projections and exact COUNT/SUM comparisons with
persistent one/four-client connections. All 168 standard and 84 pilot records
validated. `scenarios.py` runs the owned Software-only cases; `chart_data.py`
verifies full matrices and generates current JSON/CSV plus separately labeled
historical ClickBench/aggregate CSVs. Ratios, units, fixture sizes and sample
counts are explicit. Audit with `python3 benchmarks/table-modes/audit_scenario_charts.py`.
See the report for reproduction, timing boundaries and measurement limitations.

[Merged-main full comparison](merged-scoped-2026-11-13/report.md) records all 156
validated read/SQL/Arrow/native append and creation trials on clean source
`ca685806`, after PRs #433–435. No conflicts or retries. Managed admitted
mutations serialize; these observations do not attribute a gain to one change.
Audit with `python3 benchmarks/table-modes/merged_scoped_audit.py`.

[Scoped unmanaged Arrow input](unmanaged-arrow-input-2026-11-13/README.md) records
a fresh 112-trial matched comparison. All unmanaged medians decreased 0.27–7.47%
(4.22% geometric-mean speedup); the weakest case is flat and one managed control
is 4.67% higher with overlapping ranges. The original gate passes; complete
samples, setup allocation costs, coverage precision and limitations remain visible.
Audit with `unmanaged_arrow_input_audit.py`.

[Compaction reply-wait investigation](compaction-reply-2026-11-13/report.md)
retains the unchanged control and both rejected server-counter attribution
attempts. Added INFO requests moved the wait into the preceding observer call;
per-release counters failed their visibility gate. All diagnostic code remains
unapplied, and no production speedup or causal server/transport claim follows.
Audit with `python3 benchmarks/table-modes/compaction_reply_audit.py`.

[Current main reference](current-main-next-2026-11-13/report.md) records all 156
validated read/write/creation trials on source `24f654b7`, with no conflicts or
retries. [Explicit streamed managed ingestion](streamed-publication-2026-11-13/report.md)
compares separate calls, one Arrow stream and one larger batch using that same
binary: all 24 trials validated. One stream reduced median elapsed time by
76–84% against separate small calls; it deliberately changes the managed
publication unit. The reports retain every sample, settings and limitations.
Audit with `current_main_next_audit.py` and `streamed_publication_audit.py`.

[Command-wait isolation](command-wait-2026-11-13/report.md) retains seven fixed
control/probe/pacing jobs and all 2,219 valid release observations. PING, TIME and
pure waiting moved the release subspan; pure 20 ms pacing made whole-job elapsed
worse. Diagnostics remain unapplied, with no server/transport cause or production
speedup claimed. Audit with `command_wait_audit.py`.

[Fetch page sizing](fetch-sizing-2026-11-13/report.md) retains all 168 validated
baseline/candidate trials, including a managed eight-column control. Halving the
ceiling lowered memory but slowed single-reader managed reads 26.8–54.7%, so the
candidate remains unapplied. Audit with `fetch_sizing_audit.py`.

[Latest merged-main rerun](merged-main-2026-11-13-rerun/report.md) repeats all
156 trials after PRs #415–418, source `5f759300`, built as
`v0.1.0-dev.merged-perf-2`. Every trial completed and independently validated,
with no conflicts or retries. Audit with
`python3 benchmarks/table-modes/merged_perf_rerun_audit.py`. The one-reader
unmanaged full scan used 17.8% less elapsed time and 41.3% less peak worker RSS
than the previous full run; cross-revision/time limits remain explicit.

[Previous merged-main comparison](merged-main-2026-11-13/report.md) reruns the full
156-trial read/write/creation matrix after PRs #408 and #410–413, source
`ba855f79`, built as `v0.1.0-dev.merged-perf`. Every trial completed and
independently validated without conflicts or caller retries. Run
`python3 benchmarks/table-modes/merged_perf_audit.py` to check coverage,
source/version/QPF, validation, timings and worker-RSS arithmetic. Historical
observations remain unchanged and labeled by their actual source/settings.

[Owned string-column ingestion](ingest-strings-2026-11-13/report.md) proposes a
bounded owned buffer for exact String/LargeString-to-text conversion. All 64
native and 48 construction samples pass the original allocation/time/RSS gates;
mixed allocation medians fell 89.2–99.3%, with small native timing tradeoffs
retained. Audit with `ingest_string_audit.py`.

The [managed follow-up](managed-followup-2026-11-12/report.md) compares pinned
base/candidate binaries for managed reads and writes, including a separate larger
Arrow-call control. `managed_followup.py` accepts the same private fixture/path
arguments, defaults to six read and three write repetitions after warmup, and adds
`--large-arrow-call-rows` (default 10000, versus normal 1000-row calls). Run each
binary in a separate process with a fresh owned fixture/work directory.
`managed_followup_audit.py` checks native coverage and diagnostic evidence.

[Historical main comparison](main-perf-2026-11-12/report.md) repeats the complete
read/write/creation matrix on source `f630e117` after PRs #403–405, built as
`v0.0.26-dev.main-perf`. Run `arrow_compare.py --suite all` with that pinned
library/CLI, version and fresh private paths; `main_perf_audit.py` checks this
separate evidence bundle. All 156 trials completed and validated without retries.

The [managed SQL admission fix](sql-admission-2026-11-12/report.md) separately
reruns the affected managed writers and synchronized controls on unreleased fix
source. Same-table SQL DML now shares Arrow's admission gate; all recorded writers
complete. Earlier conflicts remain labeled by the versions that produced them.

`run.py` requires PyArrow, adbc-driver-manager, redis[hiredis] and requests, an
exact release shared library/CLI, and the independently typed Arrow source
identified by SHA-256 in the result metadata. Source data and private operational
artifacts are deliberately excluded. Set `REDIS_BENCH_URI` (TLS with protocol=3),
`SSL_CERT_FILE`, `REDIS_BENCH_API_URL`, `REDIS_BENCH_API_USER`,
`REDIS_BENCH_API_PASSWORD` and optionally `REDIS_BENCH_DATABASE` (default adbc-test).
Use `make -C go redis-up` with a dedicated Software project/database; verify
4x QPF before any work. The harness rechecks effective configuration per case.

Example, using task-specific paths and environment credentials:

```sh
python run.py --source /private/source.arrow --driver /private/release/go/build/libadbc_driver_redis.dylib \
  --cli /private/release/go/build/redis-adbc --version v0.0.26 \
  --work /private/new-results --dedicated-empty-server
python audit.py
```

The database must initially be empty. The harness creates a private ownership
marker and **erases that dedicated database between write trials** only after
checking ownership. Do not use a shared or production database. A fresh work
directory is required; existing evidence is never overwritten.

`concurrency_control.py` runs four barrier-synchronized managed writers against
100 rows, using SQL INSERT and Arrow append, three trials per API. Run each
release in a separate process and a fresh dedicated empty fixture. It requires
the same environment plus `--driver`, `--version`, `--source`, `--output`,
`--source-commit` and `--dedicated-empty-server`. It checks correctness/conflicts;
it does not measure throughput. Neither harness retries SQL or Arrow input.

Known managed SQLSTATE 40001 outcomes remain failed throughput trials. Complete
outputs and visible partial rows are independently checked; no partial-result
latency is reported. Other errors stop the run. Raw logs may contain private
operational details; publish only reviewed, sanitized evidence.

`arrow_compare.py` extends the same fixture/read/validation routines for the
unreleased PR #401 snapshot. It adds matched Arrow append and separate creation
workloads. Arguments match `run.py`; `--suite` additionally accepts `creation`
and `write-all`. The default is all reads/writes/creation, six measured read and
three measured write repetitions after warmup. Use `arrow_audit.py` for its
separate evidence bundle; the historical v0.0.26 audit and measurements are retained.
See the [post-Arrow results](post-arrow-2026-11-12/report.md) for that exact source
snapshot, including the retained managed SQL conflicts.

The existing-table write comparison excludes preparation for every client.
Creation includes schema/index work: native/unmanaged indexes provide membership
only, while managed tables maintain their private indexed snapshot layout.
Creation results therefore measure the respective workloads, not equal index
semantics. Explicit keys and source conversion remain timed. Neither suite retries
failed SQL/Arrow operations or reports incomplete loads as successful throughput.

The [managed write attribution](managed-write-2026-11-12/report.md) is a separate
Go diagnostic of repeated script traffic, publication and maintenance. It retains
all timing trials and reports no demonstrated write-throughput improvement.

The [ingestion conversion reuse evaluation](ingest-buffer-2026-11-13/report.md)
retains all 24 conversion and 64 matched native samples. Reusing bounded storage
reduced wide conversion allocation bytes 65–67% and native RSS 6–15% in reuse
cases, but mixed elapsed results did not meet the adoption gate. Production Go
is unchanged; the exact candidate/tests remain unapplied. Audit with
`python3 benchmarks/table-modes/ingest_buffer_audit.py`.

## Rejected adaptive managed reply target

The [November 13 evaluation](hash-reply-target-2026-11-13/report.md) retains all
56 matched native trials of an experimental 16-MiB reply target. Peak worker RSS
fell 8.9–10.8%, but single-reader elapsed time rose 32.7–34.0%; the candidate was
rejected as the default and production policy is unchanged. This is negative
evidence, with exact source/build hashes and reproduction controls. Audit the
retained samples with `python3 benchmarks/table-modes/hash-reply-target-2026-11-13/audit.py`.

## Rejected batched Arrow builders

The [bounded bulk-append evaluation](batched-arrow-2026-11-13/README.md) preserves
all 24 construction and 112 matched native samples. Several elapsed-time medians
improved, but higher allocations and 4096-row RSS ranges above baseline failed
the predeclared memory gate. The exact candidate/tests are an unapplied patch;
production Go code is unchanged. Audit with
`python3 benchmarks/table-modes/batched_arrow_audit.py`.

The [fused pointer-input follow-up](direct-arrow-2026-11-13/README.md) preserves a
separate unapplied experiment without extra row staging: all 24 construction and
112 native trials validated, but managed timings did not meet the predeclared
both-mode benefit gate. Audit with `python3 benchmarks/table-modes/direct_arrow_audit.py`.

## Generate the scenario and chart data

`scenarios.py` uses the same private `REDIS_BENCH_*` / `SSL_CERT_FILE` environment
and verified owned Software fixture as the table-mode suite. It generates its
own deterministic four-column fixture, so it does not require the large Arrow
source file. Defaults are one warmup plus three measured rounds, 32 total point
operations or eight queries per trial, and one/four persistent clients:

```sh
python3 benchmarks/table-modes/scenarios.py \
  --driver /private/exact-build/go/build/libadbc_driver_redis.dylib \
  --cli /private/exact-build/go/build/redis-adbc \
  --version v0.1.0-dev.example \
  --work /private/new-scenario-run --dedicated-empty-server
```

The owner must use an empty dedicated database and a new work directory. The
harness retains its private ownership marker and result files for inspection;
after all clients/processes end, the owner checks that marker, empties the owned
database and tears down its fixture. Failures remain failed evidence, including
completed timed samples; SQL/input is never retried. `--suite oltp` or `olap`
selects a subset; the default `all` is needed for all chart series.

`chart_data.py` combines a complete standard table-mode run and a complete
scenario run from the **same exact driver source/version/binary**. It exports
seconds/milliseconds and baseline multiples explicitly; historical product and
before/after aggregate files use separate CSVs and metadata. The
[scenario report](scenario-charts-2026-11-13/report.md#chart-exports) contains the
complete offline export command. Do not join separate driver builds or treat
historical ClickBench values as a current Software result.
