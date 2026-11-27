# Spec

2026-11-26; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Rule

`joinPlan.reduceOuterJoins(where)` in `join.go` runs in `planSelect` (`exec.go`) after `WHERE` is bound and before `planPushdown`. It looks at each top-level conjunct of `WHERE` that has no subquery. If such a conjunct is strict in a column of the NULL-supplying item of a `LEFT JOIN`, that item becomes `INNER` and not nullable. This is Postgres's `reduce_outer_joins` for the `LEFT` case.

**Strict** means the conjunct is NULL or false whenever every column of the item is NULL, so `WHERE` drops each row the outer join added for it. `strictIn` is conservative:

| Expression | Strict in item X when |
|---|---|
| a column of X (not correlated, not a whole-row reference) | always |
| `=`, `<>`, `<`, `<=`, `>`, `>=`, `+`, `-`, `*`, `/`, `%` | either operand is |
| `AND` (and so `BETWEEN`) | either side is |
| `OR` (and so an `IN` list) | both sides are |
| `NOT`, unary `-`/`+`, `CAST` without `ON ERROR`, `IS NOT NULL` | the operand is |
| `LIKE`, `ILIKE` | the subject or pattern is |
| anything else: `COALESCE`, `CASE`, `IS NULL`, `IS DISTINCT FROM`, other functions, row comparisons, `\|\|` | never |

**Not reduced:**
- a FROM clause with a `RIGHT` or `FULL` join, isolated items or merged (NATURAL FULL) columns;
- `NATURAL` and `USING` joins, `LATERAL` items and parenthesized groups;
- `ON` conditions of later inner joins. Postgres uses those too, but this change reads only `WHERE`, which keeps the existing "outer join" case of `TestSnowflakeRejectsUnsafeShapes` rejected.

## Effect

A reduced item is planned like any inner join:
- the join can be reordered;
- `WHERE` predicates on its columns are pushed into its scan or attached as join conditions (`planPushdown` skipped them while it was nullable);
- `planSnowflakeShape` can stream the join.

`WHERE` is still evaluated in full on the joined rows. Results are unchanged by construction, and `TestReduceOuterJoinsResults` checks every case against the unreduced plan.

## Not covered

- True outer joins that need NULL-extension in the streamed join (Q40, Q75, Q78, Q80).
- DML joins (`dml.go`) and the deferred `LEFT` leaf (`deferred_left_join.go`): both still see the written `LEFT` join.
