# Snapshot read performance

Date: 2026-11-06. Requestor: repository owner (user). Author: Codex. Source: `1970bf4a49d13f70f764bf647caa6dad15dc8018`. Issue: [#338](https://github.com/redis/redis-adbc-driver/issues/338). Status: implementation and validation in progress.

Reduce avoidable read CPU/allocation overhead while retaining the statement snapshot contract. Compare current snapshots with the preceding snapshot source and the historical mutable driver. Complete Arrow results, negative performance cases and product support evidence are required. Native typed export remains an upstream request, not an available dependency.

See [spec](spec.md), [plan](plan.md) and [results](results.md).
