# Intent

Date: 2026-11-20. Source base: `27e5a8d2`. Requestor: repository user. Author: Codex.
Issue: [#554](https://github.com/redis/redis-adbc-driver/issues/554). Status: implemented; focused validation passed.

Deliver first Arrow batches from eligible live HASH collection SELECTs before reading the full source. Preserve application ownership, source keys, TTLs, exact types, full predicates and existing live consistency. No new source snapshot, Flex support or speedup claim.

See [spec](spec.md), [plan](plan.md) and [validation](validation.md).
