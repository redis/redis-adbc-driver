# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Reproduce Q22 on the 10% TPC-H subset with and without `adbc.redis.flex=true`, recording rows read and Redis commands.
2. Add `go/semijoin_flex_integration_test.go`. Confirm it fails on unmodified main.
3. Add Flex to `semiJoinState`'s eager rule in `go/semijoin.go`.
4. Run the semi-join, subquery and Flex-stopgap selections on a verified 4x QPF Software fixture, plus the unit-only pass.
5. Re-run Q22 on the subset in Flex mode with the fixed library.

No merge, release or human review approval is inferred.
