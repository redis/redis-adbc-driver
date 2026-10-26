# Redis Software proxy TLS partition validation

Date: 2026-10-21. Source: 86b8eaa, merged #399. Author: Codex; requestor:
Albert Wong (keep going with Software proxy TLS). Status: implemented;
verification in progress; human review pending.
Issue: [#400](https://github.com/alberttwong/redis-adbc-driver/issues/400).
See [spec](spec.md) and [plan](plan.md).

Close the local Software TLS/proxy coverage gap in #363 using the existing pinned
disposable one-node Software image with one or three primary shards. Exercise
independent planner/worker principals through the real proxy and management RBAC.
Keep exact values, types and failure checks; expose backend policy differences.
Do not provision Cloud/paid resources, change a system trust store, or imply
production compatibility/performance from local tests.

Also correct [#401](https://github.com/alberttwong/redis-adbc-driver/issues/401),
a hosted macOS TLS dependency-install failure caused by leaking the fixture CA
into PyPI trust. This is a one-line trust-environment fix in the same TLS workflow.
