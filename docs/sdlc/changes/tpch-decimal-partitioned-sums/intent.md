# Intent

Requested 2026-11-24: fix the open TPC-H GitHub issues. This change addresses
the aggregate cause behind [#578](https://github.com/redis/redis-adbc-driver/issues/578)
(SF1 Q1 timeout) and the grouped subquery of [#585](https://github.com/redis/redis-adbc-driver/issues/585)
(SF1 Q18 timeout). Base: `c27bb970d938d25b82e427dcfb2efe59fd40d7a9` (main).
Status: local fix, SF1 verification pending. See [spec](spec.md), [plan](plan.md),
[validation](validation.md).

TPC-H money and quantity columns are `DECIMAL(15,2)`. The bounded partitioned
aggregate accepted only integer `SUM`/`AVG` inputs, so any grouped decimal
sum (Q1's eight aggregates, Q18's `GROUP BY l_orderkey HAVING SUM(l_quantity) > 300`)
fell back to the single-threaded serial aggregate, which reads every row
through one scan. On a 10% subset Q1 issued one HASH read per row (591,805
Redis commands for 591,721 rows) and took about 10 s; SF1 is ten times larger.

Desired: grouped exact decimal `SUM`/`AVG` use the existing parallel partition
workers and external merge, with results identical to the serial path (value,
type, precision and scale), including NULL-only groups and errors. Excluded:
decimal `MIN`/`MAX` partials, decimal division inputs, server-side tuple
reduction of decimals (Search sorting vectors round numbers), joins.
