# PostgreSQL 18 JSON: intent

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong. Source: `9bc9d620`. Status: targeted batch implemented and validated.
Issue: [#98](https://github.com/redis/redis-adbc-driver/issues/98); depends on [PR #375](https://github.com/redis/redis-adbc-driver/pull/375).

Implement the eight retained JSON failures in #98 on top of PR #375. Preserve the fixed 651-case corpus, the 589 passing baseline cases, exact values, NULL semantics, typed metadata and all existing storage ownership guarantees. The broad #98 wishlist extends beyond these eight cases.

See [intent](intent.md), [spec](spec.md), and [plan](plan.md).
