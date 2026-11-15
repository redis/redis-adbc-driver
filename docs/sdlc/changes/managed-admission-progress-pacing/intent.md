# Managed admission progress pacing experiment

2026-11-14. Requestor: repository user via performance subagents. Baseline runtime b904cf8d plus attribution harness 88f9aa3a. Status: experiment rejected on measured throughput/tail/command regressions; runtime unchanged. Review: [PR #440](https://github.com/redis/redis-adbc-driver/pull/440).

The [baseline](../../../../benchmarks/managed-writer-tail/report.md) observed multi-second admission tails with 56–95 acquire attempts. Older waiters keep the 100 ms exponential-backoff cap even while successful writers publish new target generations. Test whether resetting pacing after observed target publication reduces tails without materially increasing transport/command load or reducing throughput.

See [spec](spec.md), [plan](plan.md), [validation](validation.md). No fair-queue guarantee, storage-format change, proof bypass/cache, retained pin while waiting, SQL/Arrow replay, ClickBench or unsupported servers.

Outcome: retain complete evidence and the rejected source patch; omit production pacing and its unit from the active tree. [Measured report](../../../../benchmarks/managed-admission-pacing/report.md).
