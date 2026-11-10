# Intent: preserve the Terra benchmark attempt

Date: 2026-11-10. Requestor: Albert Wong. Author: Codex.
Source driver commit: `73eb96420a20a4afc6b33a38764943be3b2ebf8c`.
Status: evidence prepared for PR review; see [spec](spec.md) and [plan](plan.md).

Record the user-requested benchmark attempt against an existing Redis Terra
instance. Distinguish a confirmed driver compatibility failure from performance
measurements, and preserve sanitized evidence without publishing operational
identifiers or connection details.

This change does not add Terra support, alter the driver, exercise multi-statement
transactions, or claim successful query/ingestion performance. Whether Terra will
expose Search or need a separate driver backend remains unresolved.

PR: [#370](https://github.com/redis/redis-adbc-driver/pull/370).
