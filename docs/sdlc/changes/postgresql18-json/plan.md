# PostgreSQL 18 JSON: plan

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong. Source: `9bc9d620`. Status: targeted batch implemented and validated.
Issue: [#98](https://github.com/redis/redis-adbc-driver/issues/98); depends on [PR #375](https://github.com/redis/redis-adbc-driver/pull/375).

1. Record focused pre-fix regressions and inspect the pinned PostgreSQL 18 oracle.
2. Add logical types and storage/Arrow/catalog propagation, then operators and set-returning/mutation functions.
3. Verify positive, negative, NULL, duplicate-key, numeric precision, parameter and round-trip cases.
4. Run unit/harness checks and relevant suites on Redis Software with verified active 4x QPF.
5. Run the unchanged full corpus, preserve every prior pass, publish sanitized results and document remaining scope.

See [intent](intent.md), [spec](spec.md), and [plan](plan.md).
