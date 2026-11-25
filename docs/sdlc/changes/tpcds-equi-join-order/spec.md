# Specification

Date: 2026-11-25. Baseline: `064eb12d`. Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649).
See [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Contract

`joinOrder` still starts an inner join from the input with the fewest candidate rows. It then adds the smallest remaining input in the best tier available:

1. An input that some condition joins with an equality `runJoin` can hash on. One side reads only inputs already joined, the other only this input. `runJoin` uses the same rule to split hash-key pairs from filters.
2. An input that only another condition reads together with the joined inputs: an OR, `<>`, `<`, or a function of columns from both sides.
3. Any other input.

Before this change, tiers 1 and 2 were equal, so a smaller input reached only through an OR or inequality could be picked over one with a hash key. That step paired every joined row with every row of the input.

Unchanged:
- Each condition is still applied at the first step where all its inputs are joined. Join results, NULL handling and condition placement do not change.
- `boundedDeferredDimension` still receives "connected by any condition", so bounded star-dimension postponement behaves as before.
- Outer and lateral joins keep the written order.

## Risks

Order changes only among inner joins, and only when both tiers are present. A hash-keyed input with many more candidate rows can now be chosen before a tiny input reached only through a filter. Each such step is still a hash join, rather than a filter applied to every row pair.
