# Specification

Date: 2026-11-25. Baseline: `252b01a4`. Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649).
See [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Contract

- `planSemiJoin` accepts a body with comma or explicit joins. It still refuses views and lazy relations. The existing rule that every outer reference is a correlation pair or residual still applies, so an outer reference in JOIN … ON, the select list or a nested subquery keeps the per-row path. The decorrelated body keeps its joins, as `dsel` already copied them. Its rows and grouping are those of the single-table case, so results are unchanged.
- **Size estimate.** For a joined body outside Flex, `semiJoinState` estimates rows as the largest input's candidates multiplied by min(1, candidates / total rows) of every other input. Counts come from each input's pushed filters, the way `joinOrder` counts. A view, lazy, grouped or nested-join input, or a count that fails, gives no estimate, and the body keeps running per row.
- **When to build.**
  - Estimated rows ≤ `semiJoinEagerRows`: the body is built up front, as small single-table bodies are.
  - Otherwise: each per-row run's wall time is added to `runTime` in `subqueryRows`. `evalSemiJoin` builds once `runTime` ≥ estimate × `semiWholeBuildRowCost` (11 µs per row). This is the ski-rental rule: with an accurate estimate, neither strategy costs more than about twice the better one.
- Joined bodies are never probed by key page (their plan is in-memory) and never cost-built by `wholeBuildByCost`. Flex keeps its existing eager build.

## Risks

The estimate assumes a star body whose dimensions join the largest input by key, with uniform keys. A badly low estimate builds a large body early. A high one only delays the build, bounded by the per-row time spent.
