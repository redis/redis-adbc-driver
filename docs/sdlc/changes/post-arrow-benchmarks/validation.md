# Validation

Completed November 12, 2026. See [intent](intent.md), [spec](spec.md),
[plan](plan.md), and the [benchmark report](../../../../benchmarks/table-modes/post-arrow-2026-11-12/report.md).

Built the detached PR #401 source commit
`426fdb88bbff8561baf5f8f3535edfe14f172985` with
`make -C go build adbc-cli VERSION=v0.0.26-dev.arrow-bench`; GetInfo and source/
binary SHA-256 checks passed. PR #401 subsequently merged as `8d6311ef` with an
identical source tree. This is an unreleased feature benchmark, not new evidence
for the historical v0.0.26 release binary. Go 1.27.3 compiled the library/CLI.

Started a dedicated Redis Software fixture using `make -C go redis-up`, then
verified product/version, one RAM shard, noeviction, 4 GiB database, active
4x QPF/six Search workers, mnp routing and 32 connections before timing and before
each case. Software 8.2.6-25 runs Redis 8.8.0/Search 8.8.8. The container had four
CPUs/eight GiB; host Apple M5 Max arm64, 18 logical CPUs/36 GiB. Data connections
used certificate-verified TLS and RESP3. Python 3.15.2, PyArrow 26.0.1, ADBC manager
1.13.0, redis-py 8.1.0, hiredis 3.5.0. Suites ran sequentially on a shared workstation.

Executed `arrow_compare.py --suite all --version v0.0.26-dev.arrow-bench` with
explicit private source/library/CLI/work paths, a fresh work directory, and
`--dedicated-empty-server`. Defaults: six measured read repetitions, three write/
creation repetitions, one warmup per case, 10K written rows and 1K-row write calls.
A separate 100-row pilot exercised each write/creation case before measurement;
pilot timings are excluded. No 100M ClickBench, physical Flex or Flight SQL run.

Results retained in sanitized measurements, including warmups:

- 84 read trials (72 measured): all complete and independently value/type/
  multiplicity validated, 100K rows x 105 columns, one/four readers, 512/4096 batches.
- 40 existing-table write outcomes (30 measured): 36 complete/validated, four
  failed managed SQL four-writer workloads (warmup plus three measured), all with
  SQLSTATE 40001. Each partial output contained 2,500 independently checked rows.
  No successful latency was assigned to incomplete workloads; no automatic replay.
- 32 creation trials (24 measured): all complete and independently validated,
  including unmanaged Arrow create and create_append. Raw application HASHes were
  also checked for every unmanaged/native case.

The creation suite includes different index/consistency work, documented in the
report. Existing-table preparation and full result validation are outside timing;
worker startup, source conversion, key/literal construction, connections, writes,
shutdown and readiness remain timed. These are workload comparisons, not isolated
driver timings or a driver-version speedup claim.

Checks:

- `python3 benchmarks/table-modes/arrow_audit.py`: full 84/40/32 coverage,
  repetition/status/validation accounting, conflict handling and recomputed summary
  arithmetic pass. Deliberately dropping a trial, altering a median and marking
  an incomplete conflict successful are each rejected.
- `python3 benchmarks/table-modes/audit.py`: historical 84 reads, 24 write
  outcomes and 12 cross-version controls still pass unchanged.
- Benchmark-environment Python `-m unittest discover -s benchmarks/table-modes
  -p 'test_*.py'`: two tests pass. An initial system-Python invocation lacked ADBC
  dependencies; the rerun used the same installed environment as the benchmarks.
- `python3 -m py_compile` for the three affected harness modules and
  `git diff --check`: pass.

Reviewed correctness/lifecycle, ownership/privacy, and evidence/scope per REVIEW.md.
No production driver behavior changes. The historical unmanaged-refusal probe is
extracted into an overridable method without changing its default behavior.
Operational identifiers, credentials, source rows, raw logs and licenses remain
private. Only this task's container, network and volumes were removed; absence
verified after cleanup, and its generated test environment file removed.
This review is not human maintainer approval.
