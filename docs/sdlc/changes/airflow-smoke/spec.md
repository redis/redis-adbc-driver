# Specification: Airflow smoke test

Date: 2026-10-29. Source: `95d6583`. Author: Codex. Status: implemented.
[Intent](intent.md), [plan](plan.md), [evidence](validation.md).

Use Airflow 3's public TaskFlow API. A load task ingests four Arrow rows into
a run-specific table; a downstream task verifies two grouped totals, a bound
predicate, and a small XCom summary through a fresh connection. Cleanup runs
with `all_done`. The executable test entry point checks every task state so an
upstream failure cannot be hidden by a successful cleanup leaf.

Opt-in pytest coverage must initialize an isolated metadata DB, run the DAG
twice, and verify AdbcHook DDL, Arrow batch insertion, parameterized SQL and
cleanup. Preserve the provider's default transaction failure as an assertion,
rather than weakening the driver's autocommit-only contract. Fixtures must not
drop existing application tables. No driver runtime behavior changes.

Acceptance is successful complete result fetch and assertions on Redis Software
with Search. Local execution does not establish distributed deployment support.
