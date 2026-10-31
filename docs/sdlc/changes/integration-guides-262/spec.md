# Specification: guide coverage and checks

Date: 2026-10-30 (America/Los_Angeles). Source base: `3fd6bf36eef8f6ab069a6ee06929e972a40c11f3`.
Requestor: repository maintainer in the Codex chat. Author: Codex.
PR: [#262](https://github.com/redis/redis-adbc-driver/pull/262). Status: local implementation and verification complete; human review pending; no human approval inferred.


Each README table row resolves to a dedicated guide. New guides contain
installation, runnable code or a linked runnable example, expected results,
ownership/cleanup and material compatibility limits. Existing guides are reused.
Common setup builds the current library and verifies Software Search and 4x QPF.
Sample data preserves NULLs and Unicode and uses unique owned table names.

The checker executes actual guide blocks, linked examples or documented app
smokes in compatible interpreters. Missing tools, failures and skipped checks
must not be reported as passes. RStudio console execution does not establish
UI automation, Superset backend checks do not establish browser workflows, and
the DuckDB scanner check does not validate the standalone extension.
See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
