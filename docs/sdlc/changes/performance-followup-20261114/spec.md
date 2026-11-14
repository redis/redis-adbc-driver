# Contract

November 14, 2026. Requested by Albert Wong; implemented and cross-reviewed by Codex subagents. [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

Native unmanaged HMGET emits a selected image only when at least one value is
non-NULL (empty strings count). All-NULL vectors use a later complete authoritative
HGETALL image, with the existing residual predicate applied afterward. Never merge
images. Zero-column/oversized requests retain native fallback. Inspect every outer
reply before fallback; only exact direct native permission/type refusals qualify,
and both native reads retain NoRetry. Live scans remain live; sparse/wide-row
performance is not established by dense fixture results.

Root-map prefetch is an archived experiment, not selected after mixed-workload
and tail regressions. Restore the original metadata proof/read path exactly; no
new prefetch memory checks, ACL probes, caches or protocol changes remain.

Managed index-proof compatibility requires direct native permission refusals. A
joined/wrapped permission plus lost reply cannot rebuild an index. Publication
readiness batching is an archived experiment, not selected for integration after
mixed native results. Preserve all positive and negative observations.

[Intent](intent.md), [plan](plan.md), [validation](validation.md).
