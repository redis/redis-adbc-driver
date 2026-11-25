# Plan

Date: 2026-11-25. Baseline: `252b01a4`. Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649).
See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Profile Q35. Subquery counters showed per-row runs and no semi-join; the CPU profile showed snowflake fact scans under `evalSubquery`.
2. Experiment: allow joined bodies built up front (Q35 3.5 s, Q69 1.0 s, Q10 0.8 s).
3. Implement the guarded rule in `go/semijoin.go` (`semiJoin.joined`, `semiJoinedRows`, `semiInputRows`, `runTime`/`buildCost`) and the run timing in `go/bind.go`.
4. Add `semijoin_joined_integration_test.go` and confirm it fails on the baseline. It compares each query with the per-row oracle and covers: comma and explicit joins, NOT EXISTS, Q35's EXISTS with an OR of two EXISTS, a LEFT JOIN body with two keys, IN, an outer reference in ON, and the adaptive build.
5. Rerun TPC-DS Q1–Q99 at SF 0.1, A/B the subquery queries against `main`, then run the unit suite and the full Go suite on Redis Software with 4× QPF.
