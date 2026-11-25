Target: redis/redis-adbc-driver — new issue
Title: Bound client memory for eager joins and full sorts: no spill path today, and the snowflake cap falls back to the eager join
Labels: enhancement
---
## Problem

Exact grouped aggregation already spills to query-local files with bounded per-worker buffers (`go/aggregate_spill.go:16-34`). Joins and sorts have no equivalent:

- "Full sorts still materialize results and have no general spill path" (`docs/redis-flex-stopgap.md:256-257`). Driver sorts are in-memory slice sorts with cancellation checks (`go/sort_cancel.go:18-21`).
- The eager join "materializes every intermediate row as maps". Before snowflake streaming, it needed 6–10 GB on a 10% TPC-H subset (`docs/sdlc/changes/tpch-snowflake-joins/intent.md:5`).
- Snowflake streaming caps retained branch rows at 4,000,000. Above that it "falls back to the eager join before any fact row is read" (`docs/sdlc/changes/tpch-snowflake-joins/spec.md:24`, `go/join_snowflake.go:36`).
- "General join streaming/spill are separate changes" (`docs/sdlc/changes/lazy-cte-join-views/intent.md:14-15`), but no issue tracks that work.
- Client OOM kills are the recorded failure for:
  - TPC-H SF1 Q5/Q7/Q8/Q9 (#581–#584)
  - TPC-DS SF1 Q8 (#594)
  - TPC-DS Q4/Q5 on a combined build (`docs/sdlc/changes/lazy-cte-join-views/validation.md:46-50`)

Not established: which SF1-or-larger workloads would exceed the 4M cap.

## Desired behavior

When a join's retained rows or a sort's input exceeds a memory budget, the driver spills to query-local files, as exact aggregation does, instead of falling back to unbounded memory. Results, ordering, NULL semantics and cancellation stay the same, and the files are removed on every exit.

## How to verify

- A test lowers `snowflakeBranchRows`, as `go/join_snowflake_test.go:257` does, and adds a sort-budget equivalent. It checks that results match the in-memory plan and that peak heap stays bounded.
- An ORDER BY over more rows than the budget returns the same rows with bounded heap.
- Unit tests pass: `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`.
