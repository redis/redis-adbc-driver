# Intent

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `19ebcb46`. Status: implemented; local verification complete; PR review pending.
Issue: [#475](https://github.com/redis/redis-adbc-driver/issues/475).

Resolve GCD/LCM signatures before NULL short circuit or row access, preserving the merged exact finite decimal arithmetic and PostgreSQL 18 advertisement.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
