# Intent

Date: 2026-11-20. Source base: `27e5a8d2`. Requestor: Albert Wong. Author: Codex.
Status: implemented locally; focused validation passed. Issue: [#555](https://github.com/redis/redis-adbc-driver/issues/555).

Expose and remove compatible application HASH fields and evolve driver-owned
indexes without unregistering the logical live table or mutating application data.
Preserve explicit data/index ownership, binary keys, exact types, TTLs, and unknown
fields. SQL DDL, type conversion and destructive source changes remain excluded.
See [spec](spec.md), [plan](plan.md), and [validation](validation.md).
