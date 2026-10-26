# Search discovery and HMGET speed experiment

Review: [PR #419](https://github.com/alberttwong/redis-adbc-driver/pull/419).

2026-10-21. Requested by Albert Wong ("keep going"). Base: merged PR
[#416](https://github.com/alberttwong/redis-adbc-driver/pull/416), `4b0fdb5`.
Issue: [#411](https://github.com/alberttwong/redis-adbc-driver/issues/411).
Status: guarded implementation and local verification complete; PR/human review pending.
See [spec](spec.md) and [plan](plan.md).

Find a repeatable elapsed-time improvement over wide Search LOAD-all on the
current RESP3 driver. Reducing bytes alone is insufficient. Preserve exact Arrow
results and cover regular and partition APIs. Keep unsuccessful experiments out
of production defaults. Exclude aggregate query optimization and guessed-key reads.
