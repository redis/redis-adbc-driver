# Plan

Date: 2026-11-25. Baseline: `064eb12d`. Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649).
See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Reproduce: run TPC-DS Q1–Q99 at SF 0.1 through the driver against DuckDB 2.0.1, then profile the timeouts with execution diagnostics and a CPU profile.
2. Add `join_order_equi_test.go`, a Q15-shaped in-memory plan with an OR and a `<>` residual, and confirm it fails on the baseline. Keep a guard test showing a filter-only link still beats an unrelated input.
3. Rank candidates in `joinOrder` (`go/join.go`) by hash key, then other condition, then none, using the new `joinPlan.hashJoinKey`.
4. Rerun Q1–Q99 for results and timing, then run the unit suite and the join integration tests on Redis Software with 4× QPF.
