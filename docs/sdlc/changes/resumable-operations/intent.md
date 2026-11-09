# Explicit resumable operations

Date: 2026-11-09. Requestor: repository owner. Author: Codex.
Issue: [#335](https://github.com/redis/redis-adbc-driver/issues/335).
Status: implementation in progress; no public resume guarantee is shipped yet.

Allow an explicit new process to inspect, resume or abort a retained operation
only after its complete immutable input/evaluated write set is durable. Reuse
captured values, metadata dependencies and physical row identities. Never restart
external discovery, reevaluate SQL/defaults or consume Arrow input twice.

The minimal snapshot contract remains independently supported. This follow-up
adds explicit operation ownership; it does not add multi-statement transactions,
Active-Active support, eviction tolerance or a storage upgrade path.

See [spec](spec.md) and [plan](plan.md).
