# Verification scope

Date: 2026-10-29. Author: Codex. Tested source: `95d6583`.
Status: checks completed locally; review pending on the containing PR.
See [intent](intent.md), [plan](plan.md), and [results](validation.md).

Run the existing adbc_scanner integration test and standalone extension suite
against a disposable Redis Software database with Search. Successful checks
must finish fetching results and preserve existing assertions. Report failures
and skips explicitly. The standalone build must match the Python DuckDB engine.

Acceptance: both suites pass without skips; record versions, commands, test
counts, cleanup and limitations. No runtime code, tests, supported-platform
claims, registry publication or performance guarantees change.
