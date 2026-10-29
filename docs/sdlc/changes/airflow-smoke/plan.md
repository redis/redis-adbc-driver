# Plan: Airflow smoke test

Date: 2026-10-29. Source: `95d6583`. Author: Codex. Status: complete locally.
[Intent](intent.md), [spec](spec.md), [evidence](validation.md).

1. Inspect Python DB-API and test environment. Completed.
2. Build the current native driver and install Airflow with official constraints
   in a separate Python 3.12 environment. Completed.
3. Add the TaskFlow example and opt-in integration checks. Completed.
4. Exercise AdbcHook and record autocommit/discovery limitations. Completed.
5. Verify checks, review diff, update the integration guide and ecosystem table.
   Local evidence is in the linked validation record.
