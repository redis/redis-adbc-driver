# Live MERGE intent

Date: 2026-11-20. Base: `27e5a8d2`. Requestor: Albert Wong. Author: Codex.
Issue: [#553](https://github.com/redis/redis-adbc-driver/issues/553).

Support MERGE on application-owned HASHes with driver-owned indexes, preserving live per-row source semantics and existing SQL planning behavior. Do not imply atomic multi-row writes, source snapshots, mixed managed/live writes, or support on Flex.

See [spec](spec.md), [plan](plan.md), [validation](validation.md).
