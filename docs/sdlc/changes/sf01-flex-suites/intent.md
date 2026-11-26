# Intent

Requested 2026-11-26 (UTC) in a development session: "rerun tpc-h SF 0.1, tpc-ds SF 0.1 and SSB SF 0.1", on the Redis Software Flex profile (chosen over RAM when asked). Base: main `a15b1e5c`. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

Context on main before this run:
- **SSB SF1 on Flex** validated 13 of 13 ([flex-ssb-compacted](../flex-ssb-compacted/validation.md)).
- **TPC-H SF1 on Flex** last validated 15 of 22, before the compaction gate and the integer-range key demands ([#648](https://github.com/redis/redis-adbc-driver/issues/648)).
- **TPC-DS** validated 99 of 99 only at SF 0.1 on a 3-shard Software RAM database, with one cold run per query and a macOS client on a published port ([tpcds-joined-semijoin](../tpcds-joined-semijoin/validation.md)). [#649](https://github.com/redis/redis-adbc-driver/issues/649) closed without an SF1 pass.

Outcome wanted: one pass of every query in all three suites at SF 0.1, on one build of current main, on the Flex profile of the SF1 records, with the harness's full protocol (warm-up, three timed runs, full fetch, independent validation).

Scope:
- Measurement only. No driver, harness or test change.
- SF 0.1 only. This says nothing about SF1, more shards, a larger RAM tier or Redis Cloud.
- Direct Redis ADBC only; DuckDB over the driver's catalog was not run.
