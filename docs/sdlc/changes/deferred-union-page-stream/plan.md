# Plan and evidence

Date: 2026-11-23. Origin: follow-up to [PR606](https://github.com/redis/redis-adbc-driver/pull/606).
See [intent](intent.md), [specification](spec.md), [integration](integration.md).

1. Preflight source cardinality and stream guarded recursive UNION projections.
2. Preserve residuals, aliases, pairwise conversions and source failure priority.
3. Compose with exact grouped star reduction; verify actual parsed selection.
4. Keep retained execution on its established budgeted path.
5. Run standalone unit tests; record live query evidence separately.

Tests use self-contained ordinary managed-directory mocks rather than a Flex
fixture. See [verification](../streaming-star-aggregate/verification.md).
Full benchmark suites remain stopped; native performance validation is separate.
