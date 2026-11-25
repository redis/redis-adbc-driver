# Intent

2026-11-25; base `0b97f07e` (main, including the sort spill of #681). Requested by the TPC-H
issue work for [#654](https://github.com/redis/redis-adbc-driver/issues/654) (join half).
Status: local change, not released. See [spec](spec.md), [plan](plan.md),
[validation](validation.md).

A streamed snowflake join retains its dimension (branch) rows in memory, capped by
`snowflakeBranchRows` (4,000,000). Above the cap it falls back to the eager join before any fact
row is read (`docs/sdlc/changes/tpch-snowflake-joins/spec.md`). The eager join materializes
every intermediate row as maps: on the 10% TPC-H subset it needed 4.6–7.1 GB for Q5/Q7/Q9, and
at SF1 it was the client OOM kill of #581–#584. So a dimension larger than the cap trades a
bounded plan for an unbounded one.

While changing it, a second fault appeared. When the parallel dimension reader reached the cap,
it stopped its workers by cancelling their shared context. That interrupted in-flight commands
and closed the execution's connections, so the eager fallback that followed failed with
`use of closed network connection`.

Desired behavior:
- When one dimension takes the retained rows past the cap, spill it and the fact to query-local
  hash partitions, and join partition by partition, with the same rows, NULL semantics and
  residual.
- Fall back as before only when spilling cannot bound memory.
- Remove the files on every exit.
- Make the cap fallback work after the parallel reader.

Out of scope:
- spilling more than one dimension, or recursively repartitioning an oversized partition;
- star (non-snowflake) shapes and their `starDimensionRows` cap;
- the eager join itself;
- materialized consumers that retain the join's output anyway (`snowflakeJoinRows` in
  `scanMem`);
- a user-facing budget option.
