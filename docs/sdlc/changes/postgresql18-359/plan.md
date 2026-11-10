# PostgreSQL 18 mismatch fixes: plan

Date: 2026-11-09. Author: Codex. Requestor: Albert Wong. Source: `08d75d50888374b805a5c89f5bb94a46cf6fe8a9` plus PostgreSQL 18 target changes. Status: implemented and triaged; pending PR review. Issue: [#359](https://github.com/redis/redis-adbc-driver/issues/359).

1. Preserve baseline IDs and add a closure check against actual report outcomes.
2. Reproduce grouped root causes with focused cases before implementing fixes.
3. Address input validation, typed literals and scalar functions; then date/time, result representation, query forms, metadata and remaining features.
4. Run focused tests per change and the curated PostgreSQL 18 corpus; inspect every remaining failure.
5. Run appropriate Software and harness checks, review the diff and publish sanitized evidence. Open the requested PR; close the issue only with its requirements accounted for.

See [intent](intent.md) and [spec](spec.md).

Implementation and case dispositions are delivered in [PR #366](https://github.com/redis/redis-adbc-driver/pull/366). See [validation](validation.md) and the [case audit](cases.md); follow-up feature issues retain deferred failures.
