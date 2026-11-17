# Plan

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `ef914bb3`. Status: implemented; local verification complete; PR review pending.
Issue: [#364](https://github.com/redis/redis-adbc-driver/issues/364).

1. Add negative harness regressions and reproduce failures.
2. Implement bounded policies without changing corpus/allowlist.
3. Run harness tests and full PostgreSQL 18 vs verified Software 4x QPF comparison.
4. Record full counts, review invariants and deliver a PR linked to #364.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
