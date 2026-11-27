# Intent

Requested 2026-11-26 (UTC) in a development session: "look at the github issues and fix the tpc-ds items". This change covers [#715](https://github.com/redis/redis-adbc-driver/issues/715) (TPC-DS SF1 Q93) and [#708](https://github.com/redis/redis-adbc-driver/issues/708) (Q49). Base: main `5adb8027`. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

In the [SF1 Flex pass](../sf1-flex-suites/validation.md) and a diagnostic rerun on main:
- **Q93** was OOM-killed at the 8 GiB client limit after reading 3.17M `store_sales` and `store_returns` rows in an eager scan.
- **Q49** exceeded the 60 s deadline after reading 1.8M rows.

Both write a sales table `LEFT OUTER JOIN` its returns table, and then filter or join on a returns column in `WHERE`:
- Q93: `sr_reason_sk = r_reason_sk`.
- Q49: `wr.wr_return_amt > 10000`, and its catalog and store branches likewise.

That `WHERE` removes every row the outer join adds with NULLs, so the outer join is an inner join. The driver kept it an outer join, which:
- fixes the written join order;
- keeps `WHERE` predicates on the returns table out of its scan;
- is refused by the streamed star/snowflake join, which admits only inner joins.

Outcome wanted: plan such a `LEFT JOIN` as the inner join it is, as Postgres does, without changing any query's result.

Scope:
- `LEFT JOIN` reduction in a `SELECT`'s `WHERE` only. No change to true outer joins (Q40, Q75, Q78, Q80) or to `UPDATE … FROM`, `DELETE … USING` or `MERGE`.
- No change to execution operators.
