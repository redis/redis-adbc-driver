# Intent

Requested 2026-11-27 (UTC) in a development session ("Q39 CTE pushdown"), after measurements showed the change would fix TPC-DS SF1 Q39 ([#705](https://github.com/redis/redis-adbc-driver/issues/705)) on Software Flex. Base: main `b77df5d7`. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

Q39 defines an inventory CTE grouped by warehouse, item and month over the year 2001. It reads the CTE twice and keeps one month from each reference: `inv1.d_moy = 1` and `inv2.d_moy = 2`. The driver materializes a WITH query once for all its references, before the referencing query's `WHERE` is planned. So it read 2.34M inventory rows, the whole year, and on Flex timed out at 60 s.

Measurements on the TPC-DS SF1 Flex fixture:
- **The two months** match 360,000 inventory rows.
- **Hand-pushed filter:** with `AND d_moy IN (1, 2)` written into the CTE, which gives the same 243 rows in DuckDB, main's library finished in 22.7 s cold and 12.8 s warm.
- **No help elsewhere:** the same hand pushdown did not bring Q4, Q64, Q75 or Q78 under 60 s.

Outcome wanted: evaluate a CTE reference with the `WHERE` conjuncts that read only it, pushed through its projection, a single derived table and `GROUP BY` grouping columns, without changing any result.

Scope:
- CTE references in FROM lists of inner and comma joins.
- No pushdown into derived tables written in FROM, views, set-operation bodies or outer joins.
- No change to how CTEs are cached for references without pushable conjuncts.
