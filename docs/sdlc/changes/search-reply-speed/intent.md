# Search reply speed

2026-10-21, requested by Albert Wong: optimize elapsed time under #409.
Base `6495522` includes #412's memory change. RESP3 is already the default and
remains so. Use explicit RESP3 for ADBC and native comparison clients.
Implemented in `99c1141b`; local verification and timing complete, PR review pending.
See [spec](spec.md) and [plan](plan.md).

Reduce redundant raw Search reply maps/copies without changing Redis retrieval,
SQL plans, result values, ownership or readiness/lifecycle guarantees. Profile
the current driver, then require a repeatable measured elapsed-time improvement.
