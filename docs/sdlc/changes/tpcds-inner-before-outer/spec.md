# Specification

Date: 2026-11-25. Baseline: `252b01a4`. Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649).
See [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Contract

Let n be the number of leading items that are INNER, CROSS or comma joins, and not LATERAL items or table functions.

- **All items are inner** (n is every item): behaviour is unchanged.
- **Every later item is a plain LEFT JOIN** (not LATERAL, not isolated), and n ≥ 2: the first n items are ordered with the inner-join algorithm, using only their own ON and WHERE conditions. The LEFT JOINs follow, in written order, each with its own conditions.
- **Otherwise**, written order is kept. That covers a RIGHT or FULL join, an INNER join after a LEFT JOIN, a LATERAL item, and fewer than two leading inner items.

This is valid because a LEFT JOIN reads the complete result of the items before it. Inner joins commute among themselves, so the order inside that prefix doesn't change the rows any LEFT JOIN sees.

Conditions:
- Every condition of a prefix item reads only items up to its own position. WHERE conjuncts are attached to the last item they read, and never to the NULL-supplying side.
- If any prefix condition still read an item outside the prefix, `joinOrder` falls back to written order rather than drop it.
- Later items get candidate count -1, so `boundedDeferredDimension` never moves them.

Execution is unchanged. `runJoin` already runs reordered inner joins, and LEFT JOINs still see every prefix item as available.

## Risks

LEFT JOIN rows are still produced and filled exactly as before. The prefix now gets the same greedy cost choice as inner-only joins, including that choice's known limits.
