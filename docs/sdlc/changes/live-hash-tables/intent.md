# Intent

Date: 2026-11-18. Requestor: user. Source: `adae05d46d04bbf5be574960b627da7f45cd564b`.
Status: implemented locally; focused verification passed. PR preparation requested; no issue created.

Add an explicit third ownership mode: application-owned HASHes with driver-owned
Search indexes and SQL metadata. SQL and Redis clients read and write the same
HASHes. Registration does not copy, rename or add internal fields to source data.
The application should not need to administer indexes.

Managed tables and managed imports retain their current snapshot guarantees.
Live tables make a different consistency promise: concurrent Redis writes bypass
SQL validation and do not participate in managed statement publication.

See the [specification](spec.md) and [implementation plan](plan.md).
