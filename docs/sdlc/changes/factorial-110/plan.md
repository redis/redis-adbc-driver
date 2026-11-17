# Plan

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`. Status: implemented; local verification complete; PR review pending.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

1. Capture failing signature/value/empty regressions and probe pinned PostgreSQL18.
2. Add signature resolution, exact bounded scalar arithmetic and parameter/constant planning integration.
3. Verify independent PostgreSQL matrix, Arrow/native/race tests on owned verified Redis Software4xQPF, all unit packages and unchanged curated corpus.
4. Review and document actual results, clean the fixture, submit a PR referencing #110.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
