# Flex follow-up intent

Date: 2026-11-05. Requested by Albert Wong. Base: main `4ae4700` (merged PR #323). Status: merged in [PR #331](https://github.com/redis/redis-adbc-driver/pull/331) within the narrowed Software Flex scope.

The original scope covered issues [#324](https://github.com/redis/redis-adbc-driver/issues/324) through [#330](https://github.com/redis/redis-adbc-driver/issues/330): retained replay, schema evolution, indexes, rename recovery, restart/HA, Cloud coverage, and performance measurements. The final narrowed scope skips #325/#326 and excludes Cloud #329, as recorded in the plan. Preserve default RAM behavior, exact SQL values, ownership, cancellation and existing consistency limitations. Close issues only against actual acceptance evidence. See [spec](spec.md), [plan](plan.md), and [validation](validation.md).
