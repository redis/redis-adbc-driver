# Spec

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `75220f2f`. Status: implemented; local verification complete; PR review pending.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

Scope: explicit finite NUMERIC inputs and mixed integer/NUMERIC operands, NULLs, sign/zero, differing scales, exact large values and Arrow round trips. Normalize coefficients to a common nonnegative scale; compute with big integers and reject results exceeding Arrow decimal256 precision 76 with 22003. Preserve integer behavior and argument ownership. Nonfinite NUMERIC remains #362; untyped/ambiguous overload resolution, float/text inputs and broader formatting remain outside this batch. Resolver errors are tracked in [#475](https://github.com/redis/redis-adbc-driver/issues/475).

Reference: [PostgreSQL 18 math functions](https://www.postgresql.org/docs/18/functions-math.html).

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
