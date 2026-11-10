# Query-profile documentation: validation

Date: 2026-11-10. Source: `4768ed3c` (main after PR #366). Author: Codex. Requestor: Albert Wong. Issue: [#118](https://github.com/redis/redis-adbc-driver/issues/118). Status: documentation proposal; pending review.

Documentation-only change. No SQL implementation, test manifest, source corpus, comparison rule, allowlist or runtime configuration changes are included. The profile remains proposed and unimplemented.

Validation: local Markdown file targets resolve; category totals reconcile with the committed #359 results/inventory (77 total failures = 66 retained deferrals + 11 original storage failures; storage category 12 = 11 + one retained unique-index case); the retained inventory has 122 observed passes and 66 deferred cases. git diff --check passes. No Redis or SQL suites were rerun for this prose-only change; the linked measurements retain their actual source revision and configuration.

Review followed REVIEW.md for claim accuracy, evidence limits and scope. Historical measurements, source provenance and remaining failures are preserved. External references were read from primary PostgreSQL, NIST, SQLite/sqllogictest and Apache Arrow documentation during the discussion; absence of a discovered complete public Core suite is explicitly qualified. Agent review is not human approval.

See the [proposal](../../../postgresql18-query-profile.md), [intent](intent.md), [spec](spec.md), and [plan](plan.md).
