# Multi-shard managed ingestion: intent

Date: 2026-11-20. Requestor: Albert T. Wong. Author: Codex.
Base for this revision: `bc6780e9`. User continuation: continue work on #568 on
2026-11-20, following the request to work on all five ingestion issues.
Status: concrete coordination contract and bounded executable model delivered;
real multi-primary runtime validation pending. Production behavior unchanged.
Issue: [#568](https://github.com/redis/redis-adbc-driver/issues/568).

## Problem and outcome

Managed row HASHes, directories and publication currently share `{meta}`. Four
Arrow callers can overlap work but cannot distribute those row commands among
physical primaries. Specify a path to parallel private writes for one logical
table while preserving exact statement snapshots, ownership and non-replaying
input semantics.

The revised approach uses immutable partition generations, one authoritative
central reference/publication decision, and terminal reclamation authorization.
It does not require coordinated live read views for these immutable managed
objects. Native read views remain relevant to the separate #339/#340 work.

## Scope and delivery

Correct the earlier overconservative dependency assessment. Define prepare,
seal, publication, pins, delayed GC and failure/durability assumptions precisely;
continue with an executable bounded reference model before runtime integration.
Cross-shard durability/history rollback, local fencing, exact reads and compatible
backup/restore are engineering obligations to prove, not unavailable live-view
APIs or a new permission request arising from these generated documents.

No distributed driver implementation, full-core speedup, real multi-primary
measurement or new backup compatibility is claimed by this documentation.
No server, AWS or operational ingestion changes accompany it.

See [specification](spec.md), [design](design.md) and [plan](plan.md).
