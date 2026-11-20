# Exact-directory aggregate workers

Date: 2026-11-20. Initial source baseline: `845c95b4`; rebased onto `1d0d0b3b`. Requestor: Albert Wong. Author: Codex.
Status: implemented and locally validated; awaiting human review. No release recorded.

[Issue #551](https://github.com/redis/redis-adbc-driver/issues/551) requests bounded parallel managed aggregate reads on immutable row-ID directories, including compatible Software Flex. Existing range workers exclude Flex and nonindexable overlays. Preserve exact SQL results and snapshot visibility; do not infer a throughput improvement.

See [spec](spec.md), [plan](plan.md), and [validation](validation.md).
