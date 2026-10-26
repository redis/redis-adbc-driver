# Real-primary TLS partition workers

Date: 2026-10-21. Source: merged #414. Author: Codex; requestor: Albert Wong (keep going). Refs [#363](https://github.com/alberttwong/redis-adbc-driver/issues/363). Status: implemented; live runs validated and fixtures cleaned up; human review/hosted checks pending.

Validate direct routing across three primary endpoints, beyond Cloud’s single advertised proxy owner. Use an owned local OSS Cluster fixture, verified TLS and independent restricted workers. Preserve RESP3 default. See [spec](spec.md) and [plan](plan.md).
