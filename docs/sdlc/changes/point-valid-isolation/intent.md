# Point-validation response isolation: intent

Date: 2026-11-14. Requestor: repository user asking for performance work with
subagents. Baseline: `d7c0978f` (merged PR #440). Status: native and standalone
diagnostics executed; fixed-grouping candidate rejected on wide-response
regressions; runtime unchanged. Integration Review: [PR #441](https://github.com/redis/redis-adbc-driver/pull/441).

The earlier [native sequence](../../../../benchmarks/point-valid-sequence/README.md)
reproduced a roughly 13 ms first validation response after three native index
proof pairs, with zero pool wait. It did not identify which request or response
sequence caused the delay. Isolate command kind, serial/pipeline execution,
same/distinct indexes and downstream PING versus native pin validation before
proposing a driver change.

The diagnostic retains real immutable descriptors, a real reader pin and normal
SQL result/type oracles. It does not remove production proofs, change server
configuration or infer server CPU from client elapsed time. See [spec](spec.md)
and [plan](plan.md).
