# Shared expression traversal

Date: 2026-11-09. Requestor: repository owner. Author: Codex. Base: `cccfab5cf4eee96d491f196b21a10fec3801d9e7`. Issue: [#342](https://github.com/redis/redis-adbc-driver/issues/342). Status: in progress.

Remove the duplicated child switch in `walkOutsideAggregates` by reusing
`walkExprPruned`. Keep SQL scope boundaries and aggregate pushdown/fallback
eligibility explicit. This is a maintainability change, not a reproduced
wrong-result correction or an expansion of SQL support.

See [spec](spec.md), [plan](plan.md), [validation](validation.md).
