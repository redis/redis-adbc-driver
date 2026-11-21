# Long managed ClickBench streams: intent

Date: 2026-11-20. Source: 1b5918e6. Status: implemented; review pending.
Requestor: Albert Wong; implementation: Codex. No human approval inferred.
Issue: https://github.com/redis/redis-adbc-driver/issues/566

## Problem and outcome

Batch-sized append calls create many segments and repeated admission/compaction waits. Add an explicit one-append-per-worker strategy, retaining the existing default.

See [spec](spec.md) and [plan/evidence](plan.md).
