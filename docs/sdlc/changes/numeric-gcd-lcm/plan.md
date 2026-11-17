# Plan

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `75220f2f`. Status: implemented; local verification complete; PR review pending.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

1. Reproduce finite fractional failures and retain focused regressions.
2. Implement exact decimal coefficient arithmetic without float conversion.
3. Compare finite/NULL/sign/scale/large/boundary cases through PostgreSQL 18 and native Arrow on verified Software 4x QPF.
4. Preserve full corpus counts and record limits/review.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
