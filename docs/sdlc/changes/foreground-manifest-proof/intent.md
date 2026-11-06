# Foreground snapshot manifest validation

Date: 2026-11-06. Requestor: user. Issue: [#336](https://github.com/redis/redis-adbc-driver/issues/336).
Baseline: `2eeb82d93636f653f3feccec704863bd7fe9b35a`.
Status: implementation and validation in progress.

Reduce the fixed cost of foreground UPDATE and Arrow ingestion after the minimal
snapshot delivery. New controlled profiles show repeated serial immutable-root
manifest/count reads in both acquisition and private-workspace creation. Pipeline
those existing reads without removing validation or introducing new storage,
permissions, Lua protocols or retry behavior. Preserve no-op, unrelated-catalog,
publication/conflict, ownership, authorization and uncertain-outcome semantics.
See [spec](spec.md) and [plan](plan.md). Sustained compaction and input resume
remain separate follow-ups; no performance-parity target is implied.
