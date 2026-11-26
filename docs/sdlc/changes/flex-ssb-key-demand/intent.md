# Intent

Requested 2026-11-25 by the session working on issues #648, #658, #636, #663 and #665 for the user. The task: make the Flex streamed join's fact key demand cheaper, so SSB SF1 stops timing out on Redis Software Flex ([#658](https://github.com/redis/redis-adbc-driver/issues/658)'s SSB part, [#636](https://github.com/redis/redis-adbc-driver/issues/636)). Base: main `844d8123`, which includes [#689](https://github.com/redis/redis-adbc-driver/pull/689). See [spec](spec.md), [plan](plan.md), [validation](validation.md).

[#689's record](../flex-search-union-cost/validation.md#ssb-sf1-658) measured SSB SF1 on Flex (1 shard, 8 GiB RAM tier of 64 GiB, verified 4x QPF): only Q1.2 and Q1.3 validated, against all 13 on Software RAM with v0.0.31. A NUMERIC key-union term on `lineorder` costs about 0.6 s per Search evaluation there:
- 100 `lo_suppkey` terms took 58–62 s, and 100 `lo_orderdate` terms 55 s.
- The same 365 dates of 1993, as 12 monthly range terms, took 7.5 s (908,288 matches).
- This change's probes show that the cost is per term, point or range, almost whatever the term matches; see [validation](validation.md#why-the-other-queries-time-out).

Two plan changes follow from that:
1. **Ranges.** A fact key demand on an integer column pushes one `[v v]` term per key. SSB's `yyyymmdd` dates make consecutive runs: a month is one range.
2. **Unfiltered dimensions.** Since #644, a Flex demand may hold up to 4,096 keys. Q2.x then pushes all 2,556 `date_` keys, which exclude no fact row the join keeps.

Scope:
- Flex only. RAM databases keep their existing query strings and limits.
- Exact rewrites only: the demand stays in the fact's complete predicate, and the bucket probes still enforce the join.
- Correlated subqueries keep `maxUnionTerms`, as #689 set. The 4,096-key limit is unchanged for filtered dimensions.
- Not a cost model. Supplier and customer demands keep their per-key terms. On this deployment those can cost more than the deadline; see [validation](validation.md).
