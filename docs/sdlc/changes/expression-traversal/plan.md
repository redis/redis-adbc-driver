# Plan

Date: 2026-11-09. Base: `cccfab5c`. Issue: [#342](https://github.com/redis/redis-adbc-driver/issues/342). Status: in progress.

1. Inspect both walkers, their consumers and binding checks; decide the structural differences explicitly.
2. Characterize preorder/pruning/scopes with AST tests and baseline SQL results/eligibility before the refactor.
3. Replace the duplicate switch with the aggregate-pruning wrapper around `walkExprPruned`.
4. Run focused and broader race/planning/aggregate regressions on verified Software 4x QPF; preserve assertions and disclose skips.
5. Review, deliver a checked PR and verify #342 closure.

See [intent](intent.md), [spec](spec.md), [validation](validation.md).
