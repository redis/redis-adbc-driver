# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Add `go/join_snowflake.go`. It plans the snowflake shape: admission, condition classification, the hub fact, the BFS tree with the largest-parent rule, branches with pre-join plans, and residual placement. It also holds the broad-fact gate and the parallel dimension reader.
2. Extend `go/join_stream.go`. `scanStarJoinPages` delegates to `streamJoinPages`, which falls back from the star to the snowflake shape. Multi-item branches are built with `runJoin`. Large single-item branches are read in parallel. Retained rows are compacted and capped, and the serial probe checks the residual. Streaming `GROUP BY` also accepts DATE.
3. Extend `go/join_star_probe.go` so worker-owned probes check the residual in both `walk` and `ownedRow`.
4. In `go/join_stream_parallel.go`, admit broad snowflake facts to the parallel reader and report snowflake diagnostics.
5. In `go/query.go`, have `scanMem` try `snowflakeJoinRows` before `runJoin`. The full WHERE re-check is unchanged.
6. In `go/join_common.go`, derive implied per-column literal disjunctions from `OR` branches.
7. Tests:
   - `go/join_snowflake_test.go`: in-memory differential tests and planner unit tests, plus the pre-fix regression.
   - `go/join_snowflake_integration_test.go`: a managed-table differential test with the parallel readers.
8. Measure Q3/Q5/Q7/Q8/Q9 (and the Q19 control) on the shared 10% subset fixture. Leave SF1 verification to the coordinating session.

No release, merge or human review approval is inferred.
