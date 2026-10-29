# Prefect integration test specification

Date: 2026-10-29. Source commits: initial test `95d6583`, public PR base `34ae626`. Status: verified locally.
Issue/PR: none created. See [intent](intent.md) and [plan](plan.md).

- Execute real Prefect flows/tasks with a temporary local API backend.
- Open and close each ADBC connection/cursor within its task.
- Assert Arrow ingestion/fetch values, NULL, Unicode and column types after a
  dependency future resolves; assert four submitted aggregation results.
- Inject failure after an autocommitted write; verify Prefect retries twice in
  total and replacement leaves exactly the intended rows.
- Assert an absent table fails the flow with SQLSTATE 42P01; verify a subsequent
  connection can query successfully.
- Use unique test table names, drop them in fixture cleanup, and skip explicitly
  when REDIS_URI is absent. No transaction rollback guarantee is implied.

This adds test coverage, dependency extras, CI and scoped documentation; no
native driver behavior or dedicated Prefect connector is changed.
