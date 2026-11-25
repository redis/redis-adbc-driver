# Intent

2026-11-25; base `2ed3b3df` (main, including #631's complete union reads). Requested by the
TPC-H issue work for [#662](https://github.com/redis/redis-adbc-driver/issues/662). Status:
local change, not released. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

A decorrelated EXISTS / NOT EXISTS / IN over a large inner side reads its inner groups by page
probes: 256-key Search unions per outer page, on parallel workers (#630, #632, #633). The
inner side is built once (`buildSemiJoin`) only when it is in memory, an overlay that cannot use
Search candidates, or read under Flex (#640). Measured on the subset, a whole build beats
probing above a few thousand to tens of thousands of distinct keys, depending on the workers
(`docs/sdlc/changes/tpch-semijoin-parallel-probes/validation.md`). A whole build of a small
inner side is much cheaper than probing thousands of keys against it, but the driver never
chose it while probes were possible.

1. When an in-memory outer side (a join's rows, `scanMem`) has more distinct keys than probing
   pays for, given the inner side's planning count and the workers the probes would get,
   build the inner side once. Results and error behavior stay unchanged.
2. Diagnose the 70.9 s first-run Q21 outlier recorded for #633, or show that it does not recur
   on current main.

Out of scope: scanned outer sides (their distinct keys are not known before probing), the probe
batch size, Flex (already whole-build), and SF1 measurements.
