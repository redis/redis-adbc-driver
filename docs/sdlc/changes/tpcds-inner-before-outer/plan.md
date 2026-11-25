# Plan

Date: 2026-11-25. Baseline: `252b01a4`. Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649).
See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Reproduce: Q72 diagnostics on the #686 build (written order, 85 GB allocated).
2. Add `join_order_outer_test.go` and confirm it fails on the baseline:
   - A Q72-shaped plan where the inner joins are reordered and the two LEFT JOINs stay last with their own conditions.
   - Written-order guards for RIGHT, FULL and an INNER after a LEFT.
3. In `joinOrder` (`go/join.go`), find the reorderable prefix. Build the condition pool from it, and fall back to written order if a condition reads beyond it. Order the prefix greedily, then append the LEFT JOINs.
4. Rerun TPC-DS Q1–Q99 at SF 0.1, A/B the queries with outer joins against `main` on the same fixture, then run the unit suite and the full Go suite on Redis Software with 4× QPF.
