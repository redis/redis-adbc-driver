# Specification

2026-10-22. Author: Codex. Base: 7a62902. Issues #476/#477/#478.

- ExecutePartitions and ReadPartition use execution-owned transports. Stream
  readers retain the pool until completion/release/cancel. Retained readers
  preserve their maximum 30-second socket timeout and no-replay policy.
- A cancelled child probe closes its pending sockets but does not permanently
  prevent the still-active execution from dialing fresh connections. A late
  dial belonging to a cancelled context is closed before entering the pool.
- WithoutCancel cleanup after interruption uses the maintenance pool. Lease
  renewal and rollback explicitly clear transport ownership so they survive
  interruption. Connection close signals and waits for rekeys before closing
  ordinary stream ownership, preserving rewrite/rename rollback errors.
- Cursor deletion uses an independent pool on the original coordinator; it
  never replays a consumed READ or deletes a cursor on a replacement node.
  A lost first reply can leave an unknown cursor until server idle expiry.
- Diagnostic socket counts follow the original connection's active diagnostics.
  Per-command accounting is unchanged; diagnostics contain no credentials.
- HASH counting remains bounded and marked approximate under concurrent DML.
  Temporary retained sources belong to the producer; readers receive only
  immutable committed output. Existing quotas/expiry/revocation are preserved.
- RESP3 is required by the current project contract. Cluster fault fixtures
  exercise client routing but do not claim a new live TLS/multi-node deployment.

Acceptance: existing type-rewrite/rekey-close rollback assertions, diagnostics,
reader-registration close race, complete live/retained Arrow fetch equivalence,
full Go and native regression checks, race-detected interruption across command,
pipeline, cursor, owner/caller cancellation and finite/disabled read timeouts.
Retain and report ACL/deployment skips. See [intent](intent.md), [plan](plan.md).
