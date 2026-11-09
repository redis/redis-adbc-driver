# Traversal contract

Date: 2026-11-09. Base: `cccfab5c`. Issue: [#342](https://github.com/redis/redis-adbc-driver/issues/342). Status: in progress.

Use the existing `walkExprPruned` child enumeration. The outside-aggregate policy
prunes an aggregate before invoking the caller or visiting its arguments,
ORDER BY and FILTER. Scalar calls, casts and conversion-error expressions,
CASE/row/field expressions and window children retain canonical preorder.
Subquery SELECT bodies remain separate scopes; only IN/ANY/ALL operands belong
to the current scope, with row operands unwrapped as before. A window's own Func
is not a group aggregate; nested aggregates in its children remain prunable.

Two structural differences are deliberately reconciled. RowColumn now exposes
its scalar Subquery node, whose SELECT body is still excluded. A shared scalar
subquery can appear once per row-column occurrence. Scalar Func ORDER BY/FILTER
children use canonical enumeration, but `resolveCall`/`checkCall` reject these
modifiers before either aggregate planner consumes a bound expression. Both
current consumers collect only current-scope ColumnRefs. Neither reconciliation
is claimed as a supported SQL behavior correction.

Preserve exactness guards, SQL errors and pushdown/fallback eligibility. Do not
migrate binding, alias/grouping rebuilders, add an AST framework or change
storage/public APIs. Verify visitor policies and complete query results on
Redis Software with active/effective 4x QPF, including aggregate and distinct
pushdown, windows, row subqueries and cast errors.

See [intent](intent.md), [plan](plan.md), [validation](validation.md).
