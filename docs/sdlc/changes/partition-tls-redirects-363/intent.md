# Intent

Date: 2026-10-21. Source: `70a0d43` (#417). Author: Codex; requestor: Albert Wong (keep going). Status: implemented; review pending. Refs [#363](https://github.com/alberttwong/redis-adbc-driver/issues/363).

The owned TLS fixture proves steady-state routing across three primaries, but
not redirects during topology changes. Validate existing independently authenticated
native connections across a deterministic slot migration, preserving exact Arrow
values and restricted worker credentials. No paid resources or runtime default
changes. Replica failover, concurrent writers, snapshots and performance are excluded.

See [spec](spec.md) and [plan](plan.md).
