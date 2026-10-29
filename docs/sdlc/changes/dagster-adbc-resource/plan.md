# Plan: community resource tests

Date: 2026-10-29. Source: `95d6583`. Author: Codex. Status: completed.
[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Inspect the published resource and local disposable Redis Software instance.
2. Build this checkout's native driver and install dagster-adbc in a temporary
   Python environment.
3. Add six integration cases in `python/tests/test_dagster_adbc.py`, include the
   community package in the test extra, and document resource configuration.
4. Run new cases together with existing Dagster IO-manager tests; inspect diff
   for schema isolation, connection/Arrow ownership and compatibility claims.

All steps completed. No PR, release or human review was requested or performed.
