# Plan

2026-11-27 (base main `b77df5d7`); see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Measure on the TPC-DS SF1 Flex fixture:
   - the inventory rows Q39's months match;
   - Q39, Q4, Q64, Q75 and Q78 with their outer filters pushed into the CTEs by hand, each checked against DuckDB.
2. Read how CTEs and derived tables are materialized: `cteRelation`, `planCTE`, `materialize` and the derived-table cache.
3. Add `cte_pushdown_test.go` on an in-memory fixture:
   - per-reference row counts for the pushable and the non-pushable cases;
   - result equality with and without the pushdown;
   - unsafe bodies that must keep every row.

   Run it with only the toggle variable, and record the failures.
4. Implement `ctePushdowns`, `pushableCopy`, `pushIntoSelect` and `pushedCTERelation`, and wire them into `planSelect` and `resolveFromItem`.
5. Run the unit-only suite.
6. On the SF1 Flex fixture, run Q39 with this change and with main.
7. Run all 99 TPC-DS queries at SF 0.1, then an A/B against main for the queries whose CTE references are pushed.
