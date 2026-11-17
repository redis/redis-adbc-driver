# Plan

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `42f6e0b1`. Status: implemented; local verification complete; PR review pending.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

1. Retain scalar/parser/type/error regressions before implementation; probe the pinned PostgreSQL 18 reference.
2. Add special-form parsing, registry/type checks, scalar execution and exact Unicode normalization/escape decoding.
3. Validate Unicode normalization vectors and independent PostgreSQL comparisons; test managed rows, empty input, bound Arrow reuse and focused races on owned Redis Software with verified 4x QPF.
4. Review semantics/ownership, document supported scope and actual results, clean up the fixture and open a PR referencing #110.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
