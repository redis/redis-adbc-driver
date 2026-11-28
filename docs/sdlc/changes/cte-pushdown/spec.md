# Spec

2026-11-27; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Which conjuncts

`ctePushdowns` (`cte_pushdown.go`) runs in `planSelect` after the SELECT's WITH list is in scope and before its FROM items are resolved. For each FROM item that names a table without a schema (a possible CTE reference), it collects the top-level `WHERE` conjuncts that meet all of these:
- every column is qualified by that item's name (alias, or table name without one), and no other FROM item has that name;
- they are copied by `pushableCopy`, which admits only:
  - comparisons (`=`, `<>`, `<`, `<=`, `>`, `>=`), `AND`, `OR`, `NOT` and non-field `IS [NOT] NULL`;
  - operands that are columns or constants: literals, parameters, and arithmetic, negation or `CAST` of constants only.

  These cannot fail on any row, so evaluating them early on rows the outer query would drop first raises no new error. Subqueries, functions (aggregate, volatile or any other), window functions, row values and correlated references are not pushed.

Nothing is collected when:
- the SELECT is a set operation;
- its FROM list has an outer join, or a `LATERAL`, `NATURAL`, `USING`, parenthesized or table-function item;
- an item has column aliases.

The copies are new nodes, over the CTE's output names; the outer `WHERE` keeps and evaluates every original conjunct.

## Pushing into a body

`pushIntoSelect(sel, columns, preds)` returns a copy of `sel` with the predicates added to its `WHERE`, or false.

**Which outputs it can push on:** a predicate is pushed only if every output name it reads is a plain, unambiguous column of `sel`'s FROM items.
- **Grouped bodies:** when `sel` groups (`GROUP BY`, `HAVING` or an aggregate), the column must also be one of its plain written `GROUP BY` columns. Filtering a group's rows before grouping then drops exactly the groups whose grouping value fails.
- **Renamed outputs:** a CTE's column list renames the outputs positionally.

**Never pushed through:** set operations, `VALUES`, `LIMIT`/`OFFSET`, `DISTINCT ON`, grouping sets (`ROLLUP`, `CUBE`, `GROUPING SETS`), windows, `QUALIFY`, `*` items, table functions, `LATERAL` and column-aliased FROM items, and bodies whose items call volatile functions.

**Derived tables:** when `sel`'s FROM is a single derived table, the pushed predicates are also pushed into it, recursively, over its output names. A new copy gets its own derived-table cache entry.

## Evaluation

`ctePushdowns` groups the references by CTE name. A CTE gets a pushdown only when **every** reference to it in the FROM list has at least one collected conjunct. Its filter is then the OR over the references of each one's AND of conjuncts, so the evaluation keeps every row any reference could keep.

In `resolveFromItem`, a reference to such a CTE goes through `pushedCTERelation`:
- **One shared copy:** the first reference plans a copy of the CTE with the pushed body (`planCTE`), uncached outside this FROM list. The CTE's other references in the list share that evaluation, as unpushed references share the CTE's. Each outer `WHERE` conjunct still applies. Evaluating per reference was measured first, and made TPC-DS Q64 (an 18-table `cross_sales` CTE read twice) 1.6× slower.
- **Falls back to the shared materialization** for:
  - a recursive or data-modifying CTE;
  - a body nothing can be pushed into;
  - a CTE that plans lazily (it reads an enclosing query);
  - a pushed body that fails to plan, unless the statement was cancelled or timed out, or Redis failed. Those errors are returned.
- **References elsewhere** (other FROM lists, subqueries) read the CTE's shared materialization, as before.
