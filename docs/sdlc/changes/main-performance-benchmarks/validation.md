# Validation and review

November 12, 2026. See [intent](intent.md), [spec](spec.md), [plan](plan.md).
Native source pinned in a detached checkout at
`f630e117201752b6ccf8c2dcb4da1d3e17203c3a`; Makefile shared-library/CLI build
with `VERSION=v0.0.26-dev.main-perf` passed. This is unreleased main including
PRs #403–405, not the historical v0.0.26 release binary.

Dedicated Redis Software started with `make -C go redis-up`; effective active
4x QPF/six Search workers, mnp/32 connections, one RAM shard, noeviction and
TLS/RESP3 verified. Database resized to 4 GiB before timing. Software image
8.2.6-25, Redis 8.8.0, Search 8.8.8; four-CPU/eight-GiB container. Host Apple M5
Max arm64, 18 logical CPUs/36 GiB; shared workstation. Private ownership,
empty-fixture and source hash guards remain in force.

Before timing, the two focused harness tests passed in the installed benchmark
environment. Historical `audit.py`, `arrow_audit.py` and `sql_admission_audit.py`
passed. Changed harness modules passed py_compile. The only harness change to
execution output is a version-derived scope label; timed worker logic, fixture
layout, validation and clock boundaries are unchanged. The shared Arrow audit
accepts explicit commit/version expectations while preserving historical defaults.

## Results and evidence checks

Executed `arrow_compare.py --suite all --version v0.0.26-dev.main-perf` with
explicit private source/library/CLI/work paths and `--dedicated-empty-server`.
The complete [report](../../../../benchmarks/table-modes/main-perf-2026-11-12/report.md)
records binary/source hashes, environment, timing boundaries and measured
distributions. Python 3.15.2, PyArrow 26.0.1, ADBC manager 1.13.0, redis-py 8.1.0,
hiredis 3.5.0; binary verified as Go 1.27.3. No driver code changes or driver
test-suite rerun is claimed for this benchmark-only change.

- 84 read trials: all complete and value/type/multiplicity validated, including
  warmups; 72 measured. Full 100K x 105 results, one/four readers, 512/4096 batches.
- 40 existing-table write trials: all complete and validated, including warmups;
  30 measured. SQL, Arrow and native HSET, 10K x 105 plus ordinal, one/four writers,
  up to 1K rows per call. No conflicts or retries, including managed SQL four-writer
  cases. Unmanaged/native source HASH values also checked independently.
- 32 creation trials: all complete and validated, including warmups; 24 measured.
  Managed/unmanaged Arrow create/create_append and native index creation + HSET.
  Index/consistency work differs as documented; no equal-index or isolated-setup
  comparison is implied. All 156 attempts retained; no skips or discarded trials.
- `python3 benchmarks/table-modes/main_perf_audit.py`: complete coverage,
  repetitions, provenance, configuration, validation accounting and recomputed
  summary arithmetic pass. Missing-trial, incomplete-write, wrong-commit and
  altered-median mutations each fail the audit as intended.
- All three historical audits pass unchanged. New/changed audit modules and
  harness pass py_compile; `git diff --check` passes. Historical evidence bundles
  have no git diff. The report exporter initially targeted the historical output
  folder; its three files were restored exactly from git before exporting to the
  separate new directory. Private observations remained intact, and historical
  provenance/arithmetic audits were rerun successfully.

Latest 4096-batch medians, one/four readers: managed 2.234/2.111 s, unmanaged
6.002/2.024 s, raw client 4.329/2.112 s. Against the historical source, the managed
one-reader case improved while the four-reader case was slower. Latest write
medians, one/four writers: managed SQL 7.886/17.783 s, unmanaged SQL 2.497/1.397 s,
managed Arrow 7.840/17.025 s, unmanaged Arrow 2.020/1.281 s, raw HSET 1.222/0.883 s.
Separate-run differences on a shared workstation do not isolate a PR's effect.
Timing includes process/import/conversion/connection/fetch or write/IPC/shutdown
work; result validation is excluded. Setup is excluded for existing tables and
included as documented for creation. Four writers make twelve calls/pipelines
versus ten for one writer; managed calls share table admission.

## Review and cleanup

Reviewed correctness/validation, ownership/privacy and evidence/scope per REVIEW.md.
No production behavior change, weakened validation or automatic input replay.
Raw Redis client is redis-py+hiredis, not the snapshot-aware NativeClient wrapper.
The historical source and actual settings remain labeled; no 100M, physical Flex,
Flight SQL or NativeClient point-read run is claimed. This is not human approval.

Removed only the task-owned container, volumes and network; absence verified.
Removed its generated test environment file. Operational identifiers, credentials,
typed source, raw logs and licenses remain private.
