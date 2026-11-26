# Plan

2026-11-26 (base main `a609aab0`); see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Find where Q75's bare `context deadline exceeded` comes from: the `context.Cause` reads of the worker pools in `parallel_topk.go` and `join_snowflake.go`.
2. Add `pool_err_test.go` with the deadline, cancel, worker-error, sentinel and `closeWith` cases. Run it against a `poolErr` that reproduces the top-k pool's current behavior, and record the failures.
3. Implement `poolErr` in `cancel.go` and use it in both pools.
4. Run the unit-only suite.
5. On a TPC-DS SF1 Software Flex fixture with verified 4x QPF, run Q75 alone with the current main library and with this change's library, and compare the reported error.
