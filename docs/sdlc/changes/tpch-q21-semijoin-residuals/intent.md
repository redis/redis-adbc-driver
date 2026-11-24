# Intent

Requested 2026-11-24: fix the open TPC-H GitHub issues. This change addresses
[#587](https://github.com/redis/redis-adbc-driver/issues/587) (SF1 Q21 exceeds the
60 s deadline in "checking bounded scan index readiness"). Base:
`e7dc1f5b8951ccebfb24bb0890a58929b10d84c2` (main, with the merged semi-join page
probes of [#630](https://github.com/redis/redis-adbc-driver/pull/630); its Go code
equals `8bb53c65`, where the checks ran). Status:
local fix, SF1 verification pending. See [spec](spec.md), [plan](plan.md),
[validation](validation.md).

Q21's two correlated subqueries each carry a second outer reference that is not
an equality:

- `EXISTS (SELECT * FROM lineitem l2 WHERE l2.l_orderkey = l1.l_orderkey AND
  l2.l_suppkey <> l1.l_suppkey)`
- `NOT EXISTS (SELECT * FROM lineitem l3 WHERE l3.l_orderkey = l1.l_orderkey AND
  l3.l_suppkey <> l1.l_suppkey AND l3.l_receiptdate > l3.l_commitdate)`

`planSemiJoin` decorrelated only bodies whose every outer reference is an
`inner = outer` conjunct, so both ran once per outer `l1` row, each run paying a
full scan setup. Q21's outer side is a four-table join (`supplier`, `lineitem
l1`, `orders`, `nation`). Correlated conjuncts are not movable into the join, so
they are evaluated on the materialized joined rows in `scanMem`, where #630's page
probes (hooked in `scanIter.keep`) never ran. On the 10% subset Q21 timed out at
120 s.

Desired: these subqueries run as semi-joins whose groups keep the inner values
the extra comparisons need, probed per page of joined rows, with unchanged
results, NULL/three-valued semantics and error behavior. Excluded: IN / NOT IN
with such comparisons, comparisons of expressions or of columns of different
classes, multi-statement or cross-statement caching, parallel probe batches
(measured as a possible follow-up), and changes to join streaming
(`join_stream.go`, `join_snowflake.go`), which another change owns.
