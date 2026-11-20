# Deferred reads over live HASH tables

Date: 2026-11-20. Requestor: Albert Wong. Author: Codex.
Source baseline: `1d0d0b3b`. Status: implemented and locally verified; human review and hosted CI pending.

[Issue #550](https://github.com/redis/redis-adbc-driver/issues/550) requests independently schedulable ADBC reads over application-owned HASHes with driver-owned indexes. Existing deferred partition planning admits only managed numeric row IDs.

Deliver an explicit bounded live-source path without creating a managed data copy, taking ownership of source HASHes, adding row IDs, or promising source snapshot isolation. Retained capture remains a separate unfinished capability. Existing Flex live-table exclusion remains.

See [spec](spec.md), [plan](plan.md), and [verification](verification.md).
