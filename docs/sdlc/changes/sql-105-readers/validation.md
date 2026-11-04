# Validation

2026-11-03. Base `6d7ac409d8c9f11163b4f4cec63e2d4da00e69f2`.
Author: Codex. In progress; no human approval claimed.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

Baseline reproduction on Redis Software 8.6.2, three shards, TLS, Search and
verified active 4x QPF: a streamed SELECT of 25,000 materialized-view rows failed
after 10,000 rows when another connection ran REFRESH CONCURRENTLY. It reported
the driver's generation-change I/O error. The regression now validates all old
row values while new queries read a different, smaller published generation.

Initial unit suite passed in all five packages. Focused reader integration and
race checks passed for full drain, multiple generations, release/cancel/connection
cleanup, abandoned owner membership, lease renewal and the existing SQL
materialized-view lifecycle. These runs preceded the additional publication-race,
Open-recovery and TRUNCATE rejection regressions.

The final patch's native library build (`make -C go build`) passed. The full
Software suite on the final patch is running at draft submission time; its
result will be recorded when complete. An earlier full run was interrupted
when the user chose to set the implementation aside, and is not a passing run.

dbt adapter work is tracked separately in redis/redis-dbt-adapter#38 and #39.
No complete dbt run, release or cross-platform validation is claimed here.
