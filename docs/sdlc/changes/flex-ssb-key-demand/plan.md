# Plan

2026-11-25 (base `844d8123`); see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Write unit tests for both changes, and confirm they fail without them.
2. Push a marked Flex demand on an integer column as runs of consecutive keys (`integerRunUnionQuery`).
3. Prove a dimension unfiltered in both shape planners (`starDimensionUnfiltered`), and cap its demand at `maxUnionTerms`.
4. Add the SSB-shaped Software integration test, star and snowflake, and confirm it fails without each change, and without the snowflake planner's flag alone.
5. Run the unit suite and the targeted Flex/Snowflake/Star/Demand integration selection on a dedicated Software fixture with verified 4x QPF.
6. Build main and this change for Linux ARM64, and run SSB SF1 on the Flex profile interleaved, in fresh client processes.
7. Record the results, including the queries that still miss and why.
