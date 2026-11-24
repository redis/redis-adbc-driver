# Plan

Date: 2026-11-23. Baseline: `07b24edb`. Issue: [#601](https://github.com/redis/redis-adbc-driver/issues/601).
See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Add atomic JSON publication and tolerant status reads in `benchmarks/olap-release/isolation.py`.
2. Preserve known engine/phase/process/OOM information and distinguish actual DuckDB timer events.
3. Add synthetic crash, corruption, replacement-failure and deadline regressions in `test_isolation.py`.
4. Run the existing no-database harness checks; reproduce focused failures against the unmodified baseline.
5. Publish the issue and reviewable PR, retaining human review as pending. Do not run/load full benchmark suites or infer successful query timings from a process exit alone.

Implementation and local validation are complete. No workflow permission changes, driver implementation changes, database operations or benchmark queries are included.
