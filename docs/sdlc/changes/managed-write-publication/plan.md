# Plan

See [intent](intent.md) and [spec](spec.md).

1. Start isolated Software through redis-up and verify effective 4x QPF.
2. Attribute fixed and sustained writes and deterministic maintenance; retain raw artifacts privately.
3. Reproduce excess work, implement a bounded correction and run focused correctness/race checks.
4. Compare matched baseline/candidate workloads and review correctness, ownership and evidence.
5. Publish sanitized evidence and remove only task-owned resources.

Steps 1–5 complete. The selected change reduces repeated script bodies, without
a demonstrated throughput improvement. See [validation](validation.md) and the
[report](../../../../benchmarks/table-modes/managed-write-2026-11-12/report.md).
Sanitized evidence published in PR #410 and task-owned resources removed.

PR: [#410](https://github.com/redis/redis-adbc-driver/pull/410).

November 13 CI follow-up: reproduce the native cursor assumption, cover both
reader paths without relaxing cancellation checks, run full native validation,
and verify the updated PR's hosted checks. See [validation](validation.md).
