# Specification

2026-11-25 (base `844d8123`); see [intent](intent.md), [plan](plan.md), [validation](validation.md).

**Ranges** (`go/query.go`). With Flex, `unionTerm` pushes a marked fact key demand (`Binary.keyDemand`, built by `starFactDimensionDemand`) on an integer column (`int16`, `int32`, `int64`) through `integerRunUnionQuery`.
- **Terms:** the distinct non-NULL keys, sorted, become one `@c:[lo hi]` term per run of consecutive integers. A single key stays `@c:[v v]`. No gap is bridged, and the int64 extremes are exact.
- **Exact:** the ranges hold exactly the demand's keys, so the index query matches the same rows as one term per key. Search stores the values as doubles, and rounding is monotonic, so a range matches at least the rows its keys match.
- **Unchanged:** the demand expression in the fact's complete predicate, re-checked on every row; the missing-value widening (`widenMissing`); the key limits; and NULL keys, which add nothing.
- **Fallback:** a key whose type is not the column's own, or any non-integer column (dates, strings), keeps `unionQuery`'s terms.
- **Scope:** only marked demands with Flex. RAM, unmarked ORs, IN lists, semi-joins and native join lookups keep one term per value.

**Unfiltered dimensions** (`go/join_stream.go`, `go/join_snowflake.go`, `go/star_fact_demand.go`). `starDimension.unfiltered` proves that a dimension retains every row of its stored tables. `starDimensionUnfiltered` sets it in both shape planners when each of the dimension's items (the dimension, or every table of its snowflake branch):
- is a stored table, not an in-memory relation (a CTE, derived table or VALUES) whose definition may have filtered its rows;
- has no filter of its own (`starShape.filters`, which include pushed and ON predicates);
- and no other condition lies within those items: in a snowflake, nothing but the branch's tree edges joins them (`cross`).

Then `starFactDimensionDemand` builds that dimension's key set only up to `min(limit, maxUnionTerms)`. Such keys exclude only fact rows the inner join drops anyway. False, the default, keeps the existing limit, so an unproven dimension is unaffected.
- **Effect:** only with Flex, whose limit (`flexDemandTerms`, 4,096) exceeds `maxUnionTerms` (1,000). Without Flex, and for correlated subqueries, the limit is already 1,000.
- **Unchanged:** an empty dimension still proves the join empty, and the bucket probes still enforce the join.
- **Trade-off:** a small dimension table can be selective without a filter of its own (few fact rows reference it). Above 1,000 keys its Flex demand is no longer pushed, as before #644.

Acceptance:
- **Ranges (unit):** `TestFlexFactKeyDemandCoalescesIntegerRuns`. For int64 and int32, a demand with runs, gaps, duplicates, NULL, negative keys and the int64 extremes gives exactly the expected ranges on Flex. RAM and an unmarked OR keep one term per key, and a missing value among the keys still widens the union. It fails without the change.
- **Existing limit test:** `TestFlexFactKeyDemandUnionLimit` uses odd keys, so its 1,500-key demand still pushes 1,500 terms.
- **Unfiltered (unit):** `TestFlexUnfilteredDimensionDemandKeepsUnionLimit`. A 1,500-key unfiltered dimension builds no demand under the Flex limit, a 1,000-key one does, and a filtered dimension's 1,500 keys do. It fails without the cap.
- **Proof (unit):** `TestStarDimensionUnfilteredProof` covers stored, branch, filtered, inside-condition and in-memory items. The snowflake fixture's CTE dimensions stay unproven.
- **Integration:** `TestSQLFlexStarKeyDemandRangesAndUnfilteredDimensions` runs on Redis with Flex, and checks rows against values computed in the test:
  - an SSB-shaped star join pushes 1993's dates as exactly 12 monthly ranges;
  - its 1,500 unfiltered suppliers are not pushed, and with a region filter they are;
  - in a snowflake branch, suppliers and nations without a filter are not pushed, a nation filter pushes them, and an unfiltered date dimension is not pushed.

  It fails with `query.go` reverted, with only the demand cap reverted, and with only the snowflake planner's flag removed.
- **SSB SF1 on Flex:** all 13 queries, a warm-up and three timed runs each under a 60 s deadline, on main and this change; see [validation](validation.md).
