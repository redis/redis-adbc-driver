# Bounded Parquet stream ownership: intent

Date: 2026-11-20. Source: 1b5918e6. Status: implemented; review pending.
Requestor: Albert Wong; implementation: Codex. No human approval inferred.
Issue: https://github.com/redis/redis-adbc-driver/issues/567

## Problem and outcome

PyArrow read-ahead defaults and long reader lifetimes can retain source buffers. Make source memory policy explicit without attributing observed RSS to an unproven driver leak.

See [spec](spec.md) and [plan/evidence](plan.md).
