# Intent: stream guarded UNION inputs into aggregate visitors

Draft implementation: [PR607](https://github.com/redis/redis-adbc-driver/pull/607).

Date: 2026-11-23. Origin: follow-up to [PR606](https://github.com/redis/redis-adbc-driver/pull/606). Status: local candidate, unreleased.
Requestor: repository user; author: Codex.

TPC-DS Q2 combines raw web/catalog sales before joining dates and accumulating
weekday sums by complete week. A guarded deferred UNION still materializes raw
rows when the aggregate visitor reaches its memory view. Provide a bounded page
visitor so the independent fact-star aggregate implementation can consume that
source without retaining all UNION or joined input rows.

Keep the shared weekly CTE's once-only complete materialization and both outer
consumer year predicates unchanged. Do not push years into raw facts, broaden
LEFT-join eligibility, alter SQL, or claim that Q2 passes before native validation.
See [specification](spec.md), [plan/evidence](plan.md).
