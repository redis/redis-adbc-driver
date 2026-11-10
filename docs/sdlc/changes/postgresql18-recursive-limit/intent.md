# PostgreSQL 18 recursive LIMIT: intent

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Source: `ce62690a`. Status: targeted implementation validated.
Issue: [#361](https://github.com/redis/redis-adbc-driver/issues/361).

Stop single-row recursive progressions when a sole direct LIMIT consumer has enough rows. Preserve exact types, UNION duplicate rules, cancellation and existing budgets. Keep all 597 previous corpus passes.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
