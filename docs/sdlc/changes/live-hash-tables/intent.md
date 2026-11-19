# Intent

Date: 2026-11-18. Requestor: user. Source: `adae05d46d04bbf5be574960b627da7f45cd564b`.
Status: implemented locally; focused verification passed. PR #510 opened; no issue created.

Add an explicit third ownership mode: application-owned HASHes with driver-owned
Search indexes and SQL metadata. SQL and Redis clients read and write the same
HASHes. Registration does not copy, rename or add internal fields to source data.
The application should not need to administer indexes.

Managed tables and managed imports retain their current snapshot guarantees.
Live tables make a different consistency promise: concurrent Redis writes bypass
SQL validation and do not participate in managed statement publication.

See the [specification](spec.md) and [implementation plan](plan.md).

Pull request: [#510](https://github.com/redis/redis-adbc-driver/pull/510).

The user subsequently requested automatic keys for live SQL inserts. Reuse
`GEN_RANDOM_UUID()` as a per-row SQL default on the virtual `__key` column,
prefixed by the registered collection prefix. Keep explicit keys and existing
unmanaged insert behavior.

Documentation follow-up: rewrite the README and ownership guide around exactly
three combinations: driver owns data and indexes; application owns both; or
application owns data and the driver owns indexes. SQL access exists in all
three. Explain managed imports as copies rather than a fourth ownership mode.

Keep the README balanced across all three ownership modes; put mode-specific
SQL/Redis examples in the corresponding guide rather than a standalone README
section that implies live access is unique to the driver-index-owned mode.
