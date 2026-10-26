# Remaining performance work

2026-10-21. Requested by Albert Wong: finish remaining #411, #410 and #406.
Baseline 7a42b2d, after merged #422. Status: #411 closed; #410 implemented and locally verified; #406 in progress.
See [spec](spec.md), [plan](plan.md).

Complete the controlled-network scan evidence, add a measured opt-in width/byte
batch budget, and reproduce/optimize exact Q2/Q4/Q9/Q10 aggregate paths. Preserve
SQL exactness, data ownership, readiness, cancellation and public API behavior.
Do not close issues based on partial evidence or extrapolate prefix timings to
the full dataset. Preserve historical full-run results and failed attempts.

#410 implementation: [PR #426](https://github.com/alberttwong/redis-adbc-driver/pull/426).
