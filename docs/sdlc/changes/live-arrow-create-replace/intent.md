# Intent

Date: 2026-11-18. Requestor: Albert Wong. Author: Codex.
Source: `393f4c0b`. Status: implemented; local validation passed.
See [specification](spec.md), [plan](plan.md) and [validation](validation.md).
PR: [#532](https://github.com/redis/redis-adbc-driver/pull/532).

Extend registered live Arrow append with creation and dedicated replacement.
The application retains ownership of source HASHes; the driver retains ownership
of its generated index and separate registration. Consumers can create a schema
from Arrow without seed HASHes and replace a dedicated collection with explicit
destructive scope/consent. Excludes managed publication, distributed source locks,
whole-call rollback, process resume and throughput/ClickBench claims.
