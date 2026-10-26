# Direct HASH values into Arrow

2026-10-21. Requested by Albert Wong ("keep going"). Baseline `a937c55b`,
[PR #419](https://github.com/alberttwong/redis-adbc-driver/pull/419).
Related [#411](https://github.com/alberttwong/redis-adbc-driver/issues/411).
Status: implementation and local confirmation complete; CI/human review pending. See [spec](spec.md), [plan](plan.md).

Improve elapsed speed of both public APIs after wide Search discovery/HMGET.
Remove materialization and lookup work only on eligible simple Arrow projections.
Preserve exact values, defaults/null markers, ownership, cancellation/readiness,
row ordering and diagnostics. Allocation-only improvements are insufficient.
Keep generic, narrow, expression, late/tie and external ownership paths intact.

Rebased onto merged #419/#420 (`a90c6b4`); source `c4a819b` matches the built
`0727c7b` production files. See [validation](validation.json) and
[report](../../../../benchmarks/hash-slice-arrow/2026-10-21/README.md).

Implementation: [PR #422](https://github.com/alberttwong/redis-adbc-driver/pull/422).
