# Direct Arrow scan construction

Date: 2026-10-21. Requested by Albert Wong in #408. Base: `cebe8088`.
Status: implementation and local verification complete; PR review pending.

Simple projections still allocate and buffer a `[]Value` per row after #404.
Build Arrow batches while decoding instead, for both regular and public
partition readers. Preserve SQL values, validation, lifecycle and fallback
behavior. No new retrieval strategy, batch-budget policy or approximate SQL.

See [spec](spec.md), [plan](plan.md) and
[issue #408](https://github.com/alberttwong/redis-adbc-driver/issues/408).
