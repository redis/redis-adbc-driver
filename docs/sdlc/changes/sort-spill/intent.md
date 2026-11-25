# Intent

2026-11-25; base `9ea7ba76` (main). Requested by the TPC-H issue work for
[#654](https://github.com/redis/redis-adbc-driver/issues/654) (sort half). Status: local
change, not released. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

A `SELECT … ORDER BY` that Search cannot sort reads every matching row into a
`map[string]Value` (about 1.4 KB per row), evaluates its keys, sorts in memory
(`selectScanned`), and then materializes the whole result. Exact grouped aggregation already
spills to query-local files (`aggregate_spill.go`); sorts had no spill path
(`docs/redis-flex-stopgap.md`). A full sort of the 600,093-row subset `lineitem` peaked at
1.4–1.7 GB of client RSS.

When a streamed sort's buffered input exceeds a memory budget, write sorted runs to
query-local files and stream the merged result, with the same rows, order (including ties),
NULL / NaN handling, OFFSET / LIMIT, error behavior and cancellation. Remove the files on every
exit. Small sorts stay on the existing in-memory path.

Out of scope, and still open in #654:
- the join half: spilling snowflake branches above `snowflakeBranchRows` and the eager join
  fallback;
- sorts outside streamed single-table SELECTs: subqueries, CTEs, joins, set operations,
  windows, DISTINCT and set-returning functions;
- sorts whose ORDER BY or select list holds a subquery, a volatile function or an aggregate;
- a user-facing budget option.
