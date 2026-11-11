# PostgreSQL 18 overlaps and input errors: intent

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Source: `61043e1a`. Status: implemented; local validation complete; pending PR review.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Resolve the two remaining #110 cases in the unchanged PostgreSQL 18 corpus: temporal OVERLAPS and float8 input error diagnostics. Preserve all 602 existing scored passes. Keep the 651-case denominator, comparison rules and allowlist unchanged. Broader #110 function coverage remains open.
