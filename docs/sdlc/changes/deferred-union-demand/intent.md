# Intent

Date: 2026-11-23. Public dependency: [PR #603](https://github.com/redis/redis-adbc-driver/pull/603), source head `79afa7d47a94460ed37190f7d6c6ee389ab13652`. Status: draft, unit-verified; live resolution of [TPC-DS Q2 #588](https://github.com/redis/redis-adbc-driver/issues/588) and [Q5 #591](https://github.com/redis/redis-adbc-driver/issues/591) remains pending.
Requested by the repository user through the focused benchmark investigation.
See [specification](spec.md), [plan](plan.md), [validation and review](validation.md).

Eligible derived/CTE UNION ALL sources currently materialize full branch outputs before their selective parent joins can restrict them. Defer only provably safe single-consumer sources so typed necessary join-key restrictions can reach branch reads. Allow small stable dimensions to be collected before a connected deferred UNION when a conservative product estimate fits the same key-demand bound. Retain real joins, duplicate multiplicity, the complete residual and statement snapshots.

This is a separate stacked optimizer draft. It contains no Flex candidate/directory, snapshot storage, parallel aggregate or scalar residual changes. No full benchmark restart or SQL-specific names, dates or rewrites are authorized by this change. A streaming UNION/aggregate follow-up is outside this draft.

Draft implementation: [PR #606](https://github.com/redis/redis-adbc-driver/pull/606).
