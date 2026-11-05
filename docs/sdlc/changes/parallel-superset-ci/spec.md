# Specification: parallel Superset CI

Date: 2026-11-04. Source: `85a9bf3`. Status: implemented; CI execution pending.
Author: Codex. Requestor: Albert Wong.

The Test workflow runs Superset in an independent Ubuntu amd64 job on the same
workflow events. Preserve Python 3.12, the upstream Superset extra and pytest
requirement, RUN_SUPERSET_TESTS, the built driver path, and both existing test
files. Provision a separate authenticated TLS Software cluster through
`make redis-up`, which verifies QPF and noeviction and exports the URI and CA.
Keep public CA roots available for dependency downloads. Collect Redis logs on
failure and clean up the disposable deployment even when tests fail.

The coverage selector, driver matrix, shared helpers, permissions and sequential
execution against each shared database remain intact. Separate provisioning may
increase runner usage; elapsed-time improvement depends on runner availability.
Acceptance: workflow lint passes, coverage-selector tests pass, and GitHub Actions
executes the relocated Superset tests successfully. No measured speedup is claimed.

See [intent](intent.md) and [plan](plan.md).
