# Specification

2026-10-22; see [intent](intent.md), [plan/evidence](plan.md).

- Preserve #187's typed-literal, date/timestamp and CASE behavior. Cover all
  seven examples, including the two COALESCE controls.
- Resolve set-operation columns containing only NULLs to VARCHAR (the driver's
  text type) at each pairwise planning boundary. Thus `SELECT NULL UNION SELECT
  NULL UNION SELECT 1` fails with a datatype mismatch. Follow PostgreSQL's
  [resolution rules and nested-UNION example](https://www.postgresql.org/docs/16/typeconv-union-case.html).
- Preserve NULL values and row multiplicities for UNION, UNION ALL, INTERSECT
  and EXCEPT; empty results retain their resolved Arrow string schema.
- Preserve #232's repeated DISTINCT output positions, aliases, mixed columns,
  repeated GROUP BY keys and subquery count. Its tuple-IN DELETE removes exactly
  the two matching rows and preserves the third row.
- Exercise the grouping cases with exact/all pushdown and driver fallback;
  assert actual index aggregation for eligible repeated-column projections.
- Leave storage ownership, query cancellation and aggregate worker behavior
  unchanged. Do not infer downstream dbt acceptance from driver tests alone.
