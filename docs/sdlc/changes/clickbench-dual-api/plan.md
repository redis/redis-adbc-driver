# Implementation and evidence

- Confirmed native API restrictions in go/partitions.go and actual internal
  execution paths in full-run diagnostics. User selected supported scan cases.
- Added scan_trial.py (actual public APIs and complete IPC consumption),
  scan_paths.py (isolated paired trials and pinned namespace/endpoint/library),
  scan_validate.py (outside-timer exact and independent checks), and seven tests.
- Local tests passed: 7/7 using pinned pyarrow 26.0.0, DuckDB 2.0.0,
  adbc-driver-manager 1.12.0 and redis 6.4.0. They cover real API dispatch through
  test doubles, full draining, reordered partitions, duplicate values/row IDs,
  schema differences, independent-oracle corruption, nulls, Unicode and typed dates.
- The client clickbench-scan-paths service is waiting for the original run.
  The snapshot service now waits for dual-path-status.json before exporting,
  and includes retained scan-data and result evidence. Full-data measurements
  are pending; passing local tests do not establish scan performance or native
  correctness on 100 million rows.

Review: no driver/data mutation; timers include matching IPC work and explicitly
record native process overhead; bounded source coverage is honestly sampled;
failed/unverified timings are null; backup and cleanup remain ordered. No PR,
release or human code-review approval is claimed. See [intent](intent.md),
[spec](spec.md), and ../../../../benchmarks/clickbench/README.md.

The complete local ClickBench helper suite passed: 85 discovered tests, 82 passed and 3 opt-in Linux isolation tests skipped on macOS. The original three isolation checks had already passed on the Linux benchmark client before loading. Full supplemental measurements remain pending. `git diff --check` passed.
