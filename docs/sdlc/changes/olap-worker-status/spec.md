# Specification

Date: 2026-11-23. Baseline: `07b24edb`. Issue: [#601](https://github.com/redis/redis-adbc-driver/issues/601).
See [intent](intent.md), [plan](plan.md), [validation](validation.md).

Publish worker, validator and coordinator checkpoint JSON by writing and fsyncing a private temporary file before atomic replacement. Missing, malformed, unreadable or non-object worker documents must not raise a coordinator JSON parse exception. Preserve the known engine, coarse phase, process result and measured cgroup OOM delta; do not invent a finer setup/execute/export phase. Invalid validator failure documents remain validation failures with no performance ratio.

Record an actual DuckDB timer event. A fired timer is a timeout with cause and 60-second metadata even if fetching races a successful return; unrelated/manual Interrupted errors remain errors. Redis statement deadlines, engine execution, independent result validation and ratio eligibility remain unchanged.

Atomic documents do not protect the coordinator from its own cgroup OOM. An independent host supervisor/checkpoint recovery is required for that distinct failure. Atomic publication is not a guarantee against whole-machine power loss. Artifacts remain trusted, private files; no endpoint, credential, process identifier or infrastructure identity belongs in public evidence.

Reviewable implementation: [PR #602](https://github.com/redis/redis-adbc-driver/pull/602).
