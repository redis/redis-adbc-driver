# Intent

Date: November 13, 2026. Requestor: Albert Wong, asking parallel work on the
next performance items. Baseline: merged main `0359c0d6` (same tree as starting
`34f13a21`). Status: harness implemented; correctness/measurements underway.

Provide reproducible attribution for [go-redis #4299](https://github.com/redis/go-redis/issues/4299)
and [Arrow Go #1548](https://github.com/apache/arrow-go/issues/1548), without
assuming a library bottleneck. Separate RESP3 parsing/materialization from
transport and Arrow C handoff from application conversion/IPC. No production
driver behavior or server compatibility change.

See [spec](spec.md), [plan](plan.md) and the
[harness guide](../../../../benchmarks/upstream-allocation/README.md).
