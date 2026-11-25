# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. In `go/join_stream.go`, stream multi-table branches with `snowflakeJoinRows` before falling back to `runJoin`.
2. Add `TestSnowflakeLargeBranchStreamsRecursively` to `go/join_snowflake_test.go`.
3. Measure the subset in Flex mode and RAM mode, on the Linux namespace route with a 4-CPU client, and profile it.
4. In `go/snapshot_delta.go`, read a ranged seek with its first directory page and stop a source at its range bound. Update the directory mocks that encoded the old command sequence (`go/snapshot_directory_mock_test.go`) and add `TestStarChunkRangeSeekReadsOneDirectoryPage`.
5. In the exclusive SF1 Flex window, measure read-only HASH throughput against client concurrency (a scratch probe), then profile Q3/Q5/Q7/Q8/Q9 one at a time, read-only. Capture driver diagnostics and Flex counters, then compare parallel reader worker counts (a fresh process starts at 4 workers on a 4-CPU client; compare 16 and 32).
6. Change worker defaults for Flex directory reads only if the measurements justify it. Done: Flex defaults to 32 workers (`go/database.go`, `TestFlexQueryWorkerDefaults`).
7. Run the harness (one warm-up, three timed runs, validation) for Q3/Q5/Q7/Q8/Q9 on SF1 Flex, record the results, and open the PR.

Outcome: the window showed cold flash reads bound every full lineitem scan. Only Q9 was run at SF1, once per worker setting, to keep the shared window short. The harness pass in step 7 was not run, because no query could meet 60 s. Raising the Flex candidate budget or union pushdown limit for Q7/Q8 is a separate planner-policy decision outside this change.
