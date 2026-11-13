# Intent

Date: 2026-11-12. Requestor: repository user asking to fix GitHub Actions.
Base: `9a385669`. Status: implemented and locally validated; GitHub CI pending.

The [failed Test run](https://github.com/redis/redis-adbc-driver/actions/runs/43362052279)
assumes unordered managed scans still open Search cursors. The managed-directory
optimization instead reads exact row directories and HASHes. Restore meaningful
CI coverage for both paths without changing production behavior or skipping tests.
See [contract](spec.md) and [plan](plan.md).

Review: [PR #409](https://github.com/redis/redis-adbc-driver/pull/409).
