# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Admission

`planSnowflakeShape` runs only where `planStarShape` rejects a streamed aggregate join, and for the materialized join of `scanMem` (derived tables such as Q7/Q8/Q9). It admits inner/comma joins without lateral, isolated, NATURAL/USING, merged, requested presence tags, views/deferred unions, NULL-supplying items or privately owned snapshot segments. Every item needs positive `starRows`. The streamed fact needs at least `snowflakeMinFactRows` (65,536) source rows, so smaller joins keep the eager plan and its row order.

Conditions are classified as follows:
- A single-item conjunct already pushed into that item's eager scan stays a filter for any predicate. Any other single-item conjunct must be pure.
- A same-type column equality between two items of an exact key type (integer, decimal, collation-free string, DATE) is an edge.
- Any other multi-item condition must be pure.
- Pure means `AND`/`OR`/`NOT` over comparisons whose operands are columns or literals of one value class (number, string, date, boolean, or NULL). An untyped string literal is a string, so it is never parsed as a date or number.
- Volatile, correlated or otherwise unclassifiable conditions reject the shape.

## Shape

The fact is the hub of the large tables. Of the items with at least half the largest item's rows, it is the one with the most distinct join neighbours, with ties going to the larger. A BFS spanning tree over the edges is rooted at the fact. A node reachable from several items of the previous layer attaches to the one with the most source rows (the referencing side of a foreign key), so Q5's customer hangs off orders rather than off supplier by nation. Non-tree edges are residual conditions. A disconnected join (a cross product) is rejected.

Each fact child roots a branch keyed by its tree edges to the fact:
- **Single-item branch:** scanned as a star dimension. With at least `snowflakeParallelDimensionRows` (65,536) source rows on a published native table, it is read with the fact's exact-directory chunk reader. The complete filter is checked on each row; rows are bucketed in chunk (row-ID) order.
- **Multi-item branch:** pre-joined by the existing eager `runJoin` over copied items restricted to the branch, with their filters pushed and their tree edges and internal residuals as join conditions.
- **Retained rows:** compacted to the consumer's columns, residual columns and the branch's own key columns (so fact demand still applies).
- **Cap:** total retained branch rows are capped at `snowflakeBranchRows` (4,000,000). Exceeding it falls back to the eager join before any fact row is read.

The fact streams through the existing serial or parallel paths. Residual conditions spanning branches or the fact are checked on every complete output row, in the serial probe and in `starRowProbe` (`walk` and `ownedRow`), with an evaluator owned by each probing worker.

A snowflake fact whose Search filter matches at least 1/8 of its row sequence (by the join order's index count) uses the parallel exact-directory reader, still checking the complete predicate on each row. Other selective facts keep the serial Search path.

`scanMem` passes no residual; it still re-checks the full WHERE on the returned rows. The aggregate path passes the WHERE residual, which the shape must classify completely.

## Related planner changes

- Streaming-star `GROUP BY` also accepts DATE columns: equal dates are identical, so representatives are unambiguous.
- `impliedJoinConjuncts` adds a column's literal disjunction when every `OR` branch restricts that column to literals. Q7's `(n1 = 'FRANCE' AND n2 = 'GERMANY') OR (n1 = 'GERMANY' AND n2 = 'FRANCE')` implies a two-value list for each nation, which is pushed into its scan; Q19's brand and container lists likewise. The complete `OR` remains a condition, and the existing `safeCommonPredicate` restrictions, nullable and isolated boundaries still apply.

## Diagnostics

Snowflake plans report `streaming_snowflake_join` (with per-dimension retained rows, cap, residual presence and parallel dimension count), or the existing `parallel_streaming_star_join` with `snowflake` and `parallel_dimensions` fields. A cap fallback reports `eager_join_fallback` before the eager join's own diagnostics.

## Acceptance

- **Regression:** a pre-fix regression (`TestSnowflakeQ5StreamsByDefault`, on unmodified main) fails, and passes after the fix.
- **Results:** snowflake and eager plans return identical row multisets and value types for Q3/Q5/Q7/Q9-shaped and materialized joins, including duplicates, NULL keys, unmatched rows, residuals, DATE keys/groups and derived tables. This holds on in-memory relations and on managed Redis tables, including the parallel fact and dimension readers.
- **Planner:** unit tests cover the Q5 tree, hub selection, cap fallback, rejections, the minimum fact size, predicate classes and implied disjunctions.
- **Regressions:** existing star/join/deferred/union/Q19 suites and the unit-only pass still pass.
- **SF1:** SF1 results are separate evidence, not inferred from the subset.
