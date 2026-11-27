# Intent

Requested 2026-11-26 (UTC) in a development session: "look at the github issues and fix the tpc-ds items", then "Yes, continue" for the true outer joins. This change covers [#706](https://github.com/redis/redis-adbc-driver/issues/706) (TPC-DS SF1 Q40) and [#714](https://github.com/redis/redis-adbc-driver/issues/714) (Q80), and is a step towards [#710](https://github.com/redis/redis-adbc-driver/issues/710) (Q75) and [#713](https://github.com/redis/redis-adbc-driver/issues/713) (Q78). Base: main `3fada6f5`. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

Each of these queries writes a sales fact `LEFT OUTER JOIN` its returns table on the fact's `(order or ticket number, item)` key. `WHERE` does not reject the returns table's NULLs:
- Q40, Q75 and Q80 read returns columns through `COALESCE`;
- Q78 keeps the sales without a return (`IS NULL`).

So the join stays an outer join, which the outer-join reduction of [#729](https://github.com/redis/redis-adbc-driver/pull/729) does not change. The streamed star/snowflake join admitted only inner joins. These queries therefore ran as eager joins or serial scans that materialize the fact:
- Q40 and Q75 timed out;
- Q80 was OOM-killed at the 8 GiB client limit;
- Q78 timed out after holding 6.8 GiB.

Outcome wanted: stream a `LEFT JOIN` whose NULL-supplying table hangs off the fact by its own key equalities, NULL-extending the fact rows it does not match, with the same results as the eager join.

Scope:
- `LEFT` only, as a leaf of the streamed shape. `RIGHT` and `FULL` joins, an outer table with other tables joined through it, and outer `ON` conditions on the preserved side stay eager.
- No change to the eager join, the aggregate or the output paths.
