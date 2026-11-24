# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Residual correlation (`go/semijoin.go`)

For an EXISTS body (NOT EXISTS is its negation), `planSemiJoin` now also accepts
top-level conjuncts `inner_col op outer_col` or `outer_col op inner_col`
(`semiResidualConjunct`), alongside at least one `inner = outer` key pair:

- `op` is `<>`, `<`, `<=`, `>` or `>=` (`=` with an outer column stays a key pair),
  and the comparison is not a nested row-item comparison.
- Both sides are plain column references: the inner one of the body itself
  (`Outer == 0`), the outer one of the immediately enclosing query (`Outer == 1`),
  neither a whole-row or outer-aggregate reference.
- The inner column's planned type and the outer column's type satisfy
  `sameEqClass`, the existing semi-join key rule. It excludes CHAR and keeps
  compared values in one class (and time unit), so `binaryOp` never fails on them.
- Every outer reference of the body is one of these pairs or residuals
  (`sq.outerUses == len(pairs) + len(resid)`). Any other outer reference, OR, NOT,
  expression, IN / NOT IN body, or any other shape keeps the per-row path.

The decorrelated body projects the key inner sides, then the residual inner
columns. Groups keep those rows (`inSet.rows`, as IN already does) in the whole
build (`buildSemiJoin`) and in page probes (`probeSemiJoin`). `evalSemiJoin`
reads the outer row's residual values (`residualOuter`). A value of another
class falls back per row. EXISTS is TRUE when some row of the key's group makes
every residual `binaryOp(op, …)` TRUE, the same function the per-row WHERE uses
for column comparisons. NULL on either side is NULL, never TRUE. A NULL key never
matches, as before. Any comparison failure, which same-class columns do not
produce, falls back to the per-row run for that outer row, so the original query
reports its own result or error. Body failures keep the existing `failed` /
`probeFailed` per-row fallback.

`semiJoinTerm` (index pushdown of a small inner side's keys onto the outer
table) still applies to positive EXISTS: its key union and "no rows" are
necessary conditions, and the conjunct is still checked on every row.

## Joined outer rows (`go/query.go`)

`scanMem` filters in-memory relations, including a join's materialized rows.
When its residual has decorrelated subqueries (`semiProbeSubqueries`), it calls
`probeSemiJoin` once per `semiProbePage` (4,096) rows before evaluating them.
Probes keep #630's bounds and rules: at most `semiProbeKeys` (256) distinct
first-key values per batch, only an indexed simple-field first key, integer keys
within exact double range, complete groups recorded only for this page's keys,
and any failure sets `probeFailed`. No join planning or streaming code changed.

## Acceptance

- Failing-before: `TestSQLSemiJoinResiduals` fails on the base with per-row runs
  and no semi-joins for every residual shape. It passes after.
- Results equal the per-row oracle (the same SQL with `AND RANDOM() >= 0` in
  each body) for NULL keys/suppliers/dates, duplicates, single- and
  multi-supplier orders, NOT EXISTS, ordering comparisons, strings, doubles with
  NaN/NULL, composite keys and mixed integer widths. Fallback shapes stay per
  row with the same results.
- A large inner side is probed in more than one bounded batch, for both a
  scanned outer table and a joined outer side.
- Q21 on the 10% subset completes and validates against DuckDB. SF1 Q21 under
  60 s on the issue profile is verified separately before closing #587.
