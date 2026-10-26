# Redis Cloud Pro partition validation

Date: 2026-10-21. Source: 35ddb9b (merged #402). Author: Codex;
requestor: Albert Wong. Status: implemented, validated and cleaned up; human review pending.
Issue: [#170](https://github.com/redis/redis-adbc-driver/issues/170).
See [spec](spec.md) and [plan](plan.md).

Validate native partitions on a dedicated Redis Cloud Pro database. The requestor
explicitly authorized a new database and directed that Cloud deployments always
default to Pro. Persist that preference in project guidance. Record actual test
results and cleanup without exposing credentials or using shared databases.
