# Managed/unmanaged table comparison

[November 12 v0.0.26 results](2026-11-12/report.md) compare managed SQL,
unmanaged SQL and redis-py+hiredis on Redis Software with verified active 4x QPF.
This directory changes benchmark tooling/evidence, not production driver behavior.
The raw Redis client is redis-py, not the managed snapshot-aware `NativeClient`.

The [managed follow-up](managed-followup-2026-11-12/report.md) compares pinned
base/candidate binaries for managed reads and writes, including a separate larger
Arrow-call control. `managed_followup.py` accepts the same private fixture/path
arguments, defaults to six read and three write repetitions after warmup, and adds
`--large-arrow-call-rows` (default 10000, versus normal 1000-row calls). Run each
binary in a separate process with a fresh owned fixture/work directory.
`managed_followup_audit.py` checks native coverage and diagnostic evidence.

[Latest main comparison](main-perf-2026-11-12/report.md) repeats the complete
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
