# Validation and review

Date: 2026-11-23. Baseline: `07b24edb3c782037f48670f6cc499d66256a59e0`.
Issue: [#601](https://github.com/redis/redis-adbc-driver/issues/601).
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Reproduced before the fix

Copied the current test module plus its report/runner helpers to a temporary directory and supplied the original `isolation.py` from baseline `07b24edb`. Ran these two synthetic tests:

- `test_malformed_crash_status_keeps_engine_code_and_oom`: coordinator raised `JSONDecodeError` on the malformed crash artifact.
- `test_duckdb_timeout_requires_actual_timer_fire`: timer-fired subcase reported `error`, failing the expected `timeout` assertion.

The unmodified baseline run exited 1 with one error and one failed subcase. No live database or real analytical query engine was invoked.

## Fixed harness

On local macOS with Python 3.15.2:

```sh
python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py' -v
```

All 14 tests passed in 3.762 seconds. They use real child processes with controlled fake adapters and no live database connections or benchmark queries. Added coverage includes malformed crash status with controlled exit 42 and simulated OOM delta 1, missing/non-object status, malformed validator status, atomic replacement failure preserving the preceding checkpoint, and timer-fired versus manual interruption. Existing isolation, reference failure, mismatch, external sorting, cleanup, redaction and report checks also passed. The existing analytical-harness CI workflow runs this command under Python 3.14; this local result does not claim a completed CI run.

## Review and limits

Agent review inspected lifecycle/cancellation boundaries, private-file publication, diagnostic handling, diff, tests and accepted scope. No actionable findings remained from those passes. This is an agent review, not human maintainer approval; human review remains pending.

Separately authorized TPC-DS issue-only baseline evidence shows Q4 exhausted its client cgroup and killed the coordinator as well as a query process. Its atomic checkpoint and worker artifact survived for independent host recovery. That observation supports the recovery boundary; it does not show coordinator OOM prevention or a query-memory fix. No Redis/SQL/container runs were performed for this publication task. Full suites remain stopped.

Reviewable implementation: [PR #602](https://github.com/redis/redis-adbc-driver/pull/602).
