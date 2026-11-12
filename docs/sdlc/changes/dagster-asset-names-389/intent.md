# Intent: distinct Dagster asset storage

Date: 2026-11-12. Author: Codex; requestor: repository user. Base: `8d4dcacd`.
Issue: [#389](https://github.com/redis/redis-adbc-driver/issues/389). Status: implemented and verified; awaiting human review.

Distinct asset keys must not overwrite each other through lossy SQL name mapping.
Preserve common simple names and prevent collisions from prefix boundaries, case,
the implicit public schema, and explicit schema overrides. No transaction or
concurrency guarantees are added. See [spec](spec.md) and [plan](plan.md).
