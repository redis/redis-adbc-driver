# Additional native candidate intent

- Date: 2026-10-30
- Initial source: `91064e6` (merged candidate runner PR #248)
- Final validated main base: `95644965357dec801b7a3b69a31908c7536330ac`
- Requestor: user
- Status: implemented and validated; PR review pending
- PR: [#258](https://github.com/redis/redis-adbc-driver/pull/258)

## Desired outcome

Exercise Ibis, MLflow, Apache Beam, Mage, and Kestra with this checkout's native
Redis ADBC shared library. Save the harnesses and dependency-compatible profiles
for future runs, retain actual failures/skips, and update the comparison evidence.

## Scope

Use disposable Redis Software with Search and active verified 4x QPF. Test
populated and empty results, types, NULLs, Unicode, real application execution,
and checked outputs. These are custom Arrow/Python integrations; no dedicated
Redis backend or hosted deployment is implied. Flight SQL is excluded.

The user also requested upstream MLflow/Beam issues for the latest PyArrow.
Reuse equivalent existing reports rather than duplicating them.

## Links

- [Specification](spec.md)
- [Plan and evidence](plan.md)
- [Previous native sweep](../candidate-test-runner/native-sweep.md)
