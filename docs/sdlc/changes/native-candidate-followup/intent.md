# Additional native candidate intent

- Date: 2026-10-30
- Source: `91064e6` (merged candidate runner PR #248)
- Requestor: user
- Status: implemented and validated; PR review pending

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
