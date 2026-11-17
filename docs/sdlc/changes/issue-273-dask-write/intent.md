# Intent

2026-11-17. Requestor: Albert Wong. Author: Codex. Source: `19ebcb46`.
Implement [#273](https://github.com/redis/redis-adbc-driver/issues/273) as a
packaged worker-local Arrow sink for Dask outputs that need not fit client memory.

Support append to caller-owned existing destinations with explicit replay and
partial-failure semantics. Exclude automatic creation/replacement, destructive
cleanup, distributed transactions, automatic resume and exactly-once claims.
The user requested issue work through subagents; no upstream acceptance is inferred.

See [spec](spec.md), [plan](plan.md), [validation](validation.md).
