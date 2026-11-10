# Shared read snapshot intent

Date: 2026-11-10. Requestor: alberttwong. Status: implementation for review.
Base source: `85509485dbc07b2e2e0ea9e1b8e08bd11a290587`.
Issue: https://github.com/redis/redis-adbc-driver/issues/365.

Airbyte reads metadata and selected tables using separate statements. Individually
atomic statements can observe different publications. Bind one finite-lived
managed catalog read view before discovery and reuse it for sequential native
ADBC reads, preserving exact schemas, values and multiplicities.

Exclude write transactions, external capture atomicity, distributed shard
coordination, native upstream views, sharing/partition workers and Flight SQL.
See [spec](spec.md), [plan](plan.md), and [verification](verification.md).
