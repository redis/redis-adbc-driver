# Spec

2026-11-26; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Admission (`planSnowflakeShape`)

An item that is the NULL-supplying side of a `LEFT JOIN` with an `ON` condition is admitted as an **outer** item when all of these hold:
- **Its `ON` conjuncts** are each either a condition on it alone, or a same-type column equality between it and one other item. The `ON` conditions on it alone become its dimension's build filters, as an `ON` filter on the NULL-supplying side does in an outer join.
- **Equalities to the fact only:** every such equality joins the fact, which is chosen as before.
  - The fact is never an outer item.
  - An outer item is therefore a leaf branch, probed by fact columns alone.
- **Other conditions are residuals.** Any other condition on it, from `WHERE` or a later inner join's `ON`, is checked on complete rows after NULL-extension. It is never a filter or a join edge, and it must be a pure predicate (`snowflakePurePredicate`, which now also admits a column's `IS [NOT] NULL`).

**Rejected:**
- an outer `ON` conjunct on the preserved side alone, or any other outer `ON` conjunct, such as a non-equality between the two sides; these decide which rows are NULL-extended, not which are kept;
- a `RIGHT` or `FULL` join, or an item nullable for another reason;
- `NATURAL`, `USING` and `LATERAL` items, as before.

## Execution

`starDimension.outer` marks the dimension. A fact row whose key is NULL, or which no dimension row matches, continues without that dimension's columns. A missing column reads as NULL (`evalEnv` uses its type), as for the rows the eager `LEFT JOIN` adds.

- **Serial probe** (`streamJoinPages`): the row goes on to the next dimension unchanged. A row that only outer dimensions touched is copied before it is emitted, as a merge would have.
- **Parallel probe** (`starRowProbe.lookup`): the outer dimension's match is one shared empty row (`outerMiss`), so `walk` and `ownedRow` merge nothing.
- **Fact key demand** (`starFactDimensionDemand`) ignores outer dimensions. An empty outer dimension no longer means an empty result.
- **No spill:** a shape with an outer dimension does not spill, because the partitioned join keeps only matched facts. Over the retained-row cap it falls back to the eager join before reading the fact.
- **Diagnostics:** the plan reports `outer_dimensions`.

## Not covered

- An outer table with children, or outer joins between dimensions.
- Q75 and Q78 at SF1 still read most of all three sales tables. Q78's CTEs aggregate every year, and `ss_sold_year = 2000` is applied only afterwards. Moving that filter into the CTEs is separate work.
