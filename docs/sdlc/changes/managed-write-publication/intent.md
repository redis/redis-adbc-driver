# Intent

Date: November 12, 2026. Requestor: Albert Wong. Baseline source: `060915bd` (the pulled local merge has the same source). Status: implemented and validated.

Reduce repeated Lua script traffic using measured foreground/publication and maintenance costs. Preserve statement atomicity, immutable rows, exact index proofs, leases, ACL behavior and no replay of uncertain mutations.

Profile fixed 1K appends separately from the existing 10K-call usage option. No 100M ClickBench run or storage-format change. See [spec](spec.md) and [plan](plan.md).

The measured result is lower repeated Lua payloads, with no demonstrated local
write-throughput improvement. See the [report](../../../../benchmarks/table-modes/managed-write-2026-11-12/report.md).

PR: [#410](https://github.com/redis/redis-adbc-driver/pull/410).
