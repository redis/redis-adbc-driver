# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

`semiJoinState` already marks a semi-join eager when its inner side is in memory, or is an overlay that cannot use Search candidates (`snapshotIndexable` false). An eager semi-join is built once with `buildSemiJoin`, the existing whole decorrelated build, before outer rows are read. The outer rows then probe its hash groups. Per-row runs and page probes (`probeSemiJoin` returns for eager states) do not run.

The change adds Flex (`store.flexEnabled()`) to that rule. On Flex every snapshot read ignores Search candidates (`snapshotIndexable` returns false), but a Flex inner table is not flagged as an overlay at planning time, so it previously took the probe path. Under Flex, a large inner side is now built once.

Everything else is unchanged: result semantics (NULL keys, three-valued IN / NOT IN, duplicates), the build's error handling (a failing build falls back to per-row runs, which report the original error), semi-join term pushdown for small inner sides, and RAM behavior.

Acceptance:
- A Flex-mode integration test (`adbc.redis.flex=true` against the RAM fixture, 2,000 inner rows above the eager limit, 2,500 distinct outer keys) fails before the change, with 10 probe batches per query, and passes after it with one whole build.
- EXISTS, NOT EXISTS, correlated IN and NOT IN results are unchanged.
- Q22 on the subset in Flex mode completes and validates.
