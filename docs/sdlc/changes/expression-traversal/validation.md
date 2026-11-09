# Evidence

Date: 2026-11-09. Base: `cccfab5cf4eee96d491f196b21a10fec3801d9e7`.
Issue: [#342](https://github.com/redis/redis-adbc-driver/issues/342).
Status: local validation complete; final CI/delivery is recorded on the PR.

## Scope decision and implementation

`walkOutsideAggregates` now delegates to `walkExprPruned`, pruning aggregate
calls before invoking its callback. The production diff removes the duplicate
child switch; the existing canonical child enumeration is unchanged. Binding,
expression evaluation, alias/grouping rebuilders, exactness guards and storage
are unchanged. No supported SQL behavior correction or feature expansion is
claimed.

The two former differences were inspected explicitly:

- `RowColumn` is constructed for scalar row subqueries by `rowOfSubquery` and
  UPDATE tuple assignment. Visiting its scalar Subquery node does not enter its
  SELECT body. Shared subqueries remain visited per row-column occurrence.
- `checkCall`, called by `resolveCall`, rejects ORDER BY/FILTER on scalar calls.
  Such children now follow canonical structural enumeration, but they cannot
  reach either aggregate pushdown consumer as accepted bound SQL.

Both production consumers (`indexAggregate` and `indexCountDistinct`) collect
only ColumnRefs with `Outer == 0`. Their current-scope references and SQL scope
boundaries remain unchanged for accepted expressions.

## Characterization and checks

| Check | Result |
| --- | --- |
| Pre-refactor visitor characterization | Existing scope/reference/pruning checks pass. Three explicit structural expectations fail: shared RowColumn Subquery-node callbacks, scalar ORDER BY child, scalar FILTER child. These are intended consolidation differences, not reproduced wrong-result bugs |
| Pre-refactor `TestSQLExpressionTraversalAggregateScopes` | Pass: complete results in default/exact/all/none modes, existing errors and positive ordinary/distinct pushdown eligibility |
| Post-refactor `TestExpressionTraversal*`, `-race -count=1` | All three top-level tests and their subtests pass |
| Full unit-only `go test -race -count=1 -p 1 -timeout=30m -json ./...`, backend URI variables unset | All six packages pass; 1,236 test/subtest passes and 1,090 backend-dependent skips; skips are not backend compatibility evidence |
| Software aggregate/planning race selection below | 42 top-level tests pass, no failures or skips |
| `gofmt`; `git diff --check`; local change-record links | Pass |

The unit fixtures cover nil input, preorder/sibling preservation, pruning the
aggregate node plus its arguments/ORDER BY/FILTER, scalar wrappers, CASE,
unary/NULL/row/field expressions, cast conversion-error expressions,
current/outer references, row IN operands without visiting their RowExpr
wrapper, scalar/row subquery body exclusion, repeated shared subquery nodes,
window arguments/modifiers/partition/order/frame offsets and nested aggregates.
A window's own Func remains excluded from group-aggregate classification.

The new SQL characterization checks nine queries across four modes, including
ordinary and DISTINCT aggregation, FILTER, nested aggregate/window calls,
row-column scalar comparisons, row IN, subquery aggregates and cast fallback
columns. It completely fetches results, retains exact error statuses/messages
for invalid scalar modifiers and nested aggregates, and explicitly checks
positive ordinary/distinct index eligibility. Existing aggregate/filter tests
retain fallback eligibility assertions.

## Redis Software validation

The task-owned disposable fixture was started/reverified with
`make -C go redis-up` before backend tests. Actual product/version evidence:
**Redis Software 8.2.6-25, Redis 8.8.0, three primary shards**, strict TLS/RESP3,
noeviction and verified active/effective **4x QPF**. Local module toolchain:
Go 1.27.3, darwin/arm64. Fixture identities, endpoints, credentials, licenses and
raw operational logs remain private.

From `go/`, through `software-setup.sh --exec`:

```sh
go test -race -count=1 -p 1 -timeout=15m \
  -run '^TestSQL(ExpressionTraversalAggregateScopes|ExactCountDistinct|Distinct.*|Agg.*|Window.*|GroupedSubquery.*|OuterAggregate.*|RowValue.*|CastShorthand)$' -v .
```

The selection covers ordinary/distinct aggregates, input ORDER BY/FILTER,
grouped subqueries and outer aggregates, windows and frames, row comparisons,
row-valued subqueries and casts, using existing pushdown/fallback regressions.
No benchmark, constant-time claim or expanded PostgreSQL compatibility is
inferred from these correctness checks.

## Review

Reviewed the diff and evidence against `REVIEW.md`: one production child switch,
an aggregate-pruning wrapper, explicit structural differences and preserved SQL
binding/scope boundaries. No actionable local finding remains. This is an agent
review, not inferred human approval; actual CI, merge and closure are recorded
on the PR.
