# Intent

Requested 2026-11-26 (UTC) in a development session: "can we run all 3 benchmarks with SF 1?", after the [SF 0.1 passes](../sf01-flex-suites/validation.md). Base: main `7c83c486`. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

Context on main before this run:
- **SSB SF1 on Flex** validated 13 of 13 on main `ac82cfa0` with compacted indexes ([flex-ssb-compacted](../flex-ssb-compacted/validation.md)).
- **TPC-H SF1 on Flex** last validated 15 of 22 on the PR #689 build, before the compaction gate and the integer-range key demands ([#648](https://github.com/redis/redis-adbc-driver/issues/648)).
- **TPC-DS SF1** had never run in full. The v0.0.31 release run stopped at Q10 ([#649](https://github.com/redis/redis-adbc-driver/issues/649)), and later Q1–Q99 checks were at SF 0.1 only.

Outcome wanted: one pass of every query in all three suites at SF1 on one build of the newest main, on the Flex profile of the earlier SF1 records, with the harness's full protocol. Each failing query is then tracked in its own issue, as [#649](https://github.com/redis/redis-adbc-driver/issues/649) asks.

Scope:
- Measurement only. No driver, harness or test change.
- Direct Redis ADBC only; DuckDB over the driver's catalog was not run.
- One fresh database per suite, so a suite's timings do not depend on another suite's data in the RAM tier.
