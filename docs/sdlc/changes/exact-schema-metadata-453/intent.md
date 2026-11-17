# Intent

2026-11-16. Requestor: Albert Wong; author: Codex. Base: `225e0056`.
Status: implemented; maintainer review pending.

[Issue #453](https://github.com/redis/redis-adbc-driver/issues/453) reports
that Arrow physical types omit SQL declaration modifiers needed by dbt and
Flight SQL consumers. Preserve the driver's canonical SQL declarations in
field metadata without changing physical Arrow types or ingestion semantics.
Do not change the Flight service or stored values.

See [spec](spec.md), [plan and evidence](plan.md).
