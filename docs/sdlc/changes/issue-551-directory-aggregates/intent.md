# Exact-directory aggregate workers

Date: 2026-11-20. Source baseline: `845c95b4`. Requestor: Albert Wong. Author: Codex.
Status: implementation and validation in progress; no human approval recorded.

[Issue #551](https://github.com/redis/redis-adbc-driver/issues/551) requests bounded parallel managed aggregate reads on immutable row-ID directories, including compatible Software Flex. Existing range workers exclude Flex and nonindexable overlays. Preserve exact SQL results and snapshot visibility; do not infer a throughput improvement.

See [spec](spec.md), [plan](plan.md), and [validation](validation.md).
