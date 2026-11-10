# PostgreSQL 18 mismatch fixes: specification

Date: 2026-11-09. Author: Codex. Requestor: Albert Wong. Source: `08d75d50888374b805a5c89f5bb94a46cf6fe8a9` plus PostgreSQL 18 target changes. Status: in progress. Issue: [#359](https://github.com/redis/redis-adbc-driver/issues/359).

The stable case list in `go/pgdiff/issue359-cases.json` defines the baseline. Every case requires an observed PostgreSQL 18 pass or an individually justified disposition with evidence; unresolved implementation gaps must remain visible. Preserve source provenance, errors, exact types, NULL/ordering semantics, Arrow ownership, cancellation, and snapshot writer/reader invariants.

Fix harness defects only where independent evidence demonstrates incorrect comparison; retain tests that distinguish real mismatches. Do not change source queries or expected values to match the driver. Validate on Redis Software with management-API-verified active 4x QPF.

See [intent](intent.md) and [plan](plan.md).
