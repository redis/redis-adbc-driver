# README purpose and value specification

Date: 2026-10-26. Status: proposed for review.
See [intent](intent.md), [plan](plan.md), and [validation](validation.md).

- Add a "Why a Redis ADBC driver?" section before compatibility details and a
  matching contents entry.
- Explain the bridge between Redis commands/HASH data and tabular SQL/Arrow
  consumers, with three concrete benefits: analysis in place, reusable analytics
  integrations, and batch exchange with schemas and NULL information.
- Link to HASH adoption, integration status, Python integrations, and requirements.
- State the Query Engine/RESP3 requirement, SQL subset, row-oriented HASH storage,
  Redis-to-Arrow conversion, autocommit, and absence of atomic source snapshots.
- Make no claims of universal tool compatibility, end-to-end zero-copy, full
  PostgreSQL compatibility, or unmeasured speedups.
- Change no runtime code, dependencies, release files, or historical results.

Review: [PR for this branch](https://github.com/redis/redis-adbc-driver/pulls?q=is%3Apr+head%3Acodex%2Fexplain-redis-adbc-value).
