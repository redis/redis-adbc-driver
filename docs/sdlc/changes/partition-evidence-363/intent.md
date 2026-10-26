# Intent

2026-10-21; requestor alberttwong; author Codex. Baseline `96f75c1`
([refactor PR #376](https://github.com/alberttwong/redis-adbc-driver/pull/376)).
Follow-up to [#363](https://github.com/alberttwong/redis-adbc-driver/issues/363).
Status: local evidence delivered; deployment/runtime gaps remain; no maintainer approval.

Compare ExecuteQuery with ExecutePartitions/ReadPartition using official
ClickBench source rows and independently checked complete results. Keep
unsupported official queries visible. Gather latency, throughput, client/server
memory and network evidence across partition/worker counts and sparse/skewed
layouts. Audit existing CI evidence and explicitly retain deployment gaps.

No Cloud provisioning or full-scale ClickBench ranking is included. See
[specification](spec.md) and [plan/evidence](plan.md).
