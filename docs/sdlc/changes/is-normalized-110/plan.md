# Plan

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`. Status: implemented; local verification complete; PR review pending.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

1. Capture failing parser/type/scalar cases before implementation.
2. Add predicate grammar and shared Unicode function result handling.
3. Compare a deterministic independent corpus against PostgreSQL 18 and test managed/empty/bound execution on owned Software with verified 4x QPF.
4. Review and document actual results, clean resources, then open a separate PR referencing #110.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
