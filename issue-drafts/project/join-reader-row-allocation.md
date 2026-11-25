Target: redis/redis-adbc-driver — new issue
Title: Reduce per-row allocation in the streaming star/snowflake join readers (GC is 40–50% of CPU on TPC-H Q9)
Labels: enhancement
---
## Problem

A CPU profile was taken of 10% subset TPC-H Q9 on the local Software RAM fixture. That query uses the parallel streaming snowflake join; the build was main with #635/#640. The profile found: "GC took 40-50% of samples. `Value` is 152 bytes, so row maps dominate allocation (about 3.4 KB per row read)." The query allocated 6.0 GB for 1.78M Redis commands (`docs/sdlc/changes/tpch-flex-read-path/validation.md:31-34`). The record adds: "This change doesn't address that."

On SF1 Flex the same query allocated 26.9–32.1 GB, with 4.8–5.2 GiB peak RSS (`:59-60`).

The join streams hold rows as `map[string]Value` (`go/join_stream.go:19,359-368`), and each `Value` embeds a full `ColType` (`go/value.go:39-53`, `go/types.go:100`). #187 (closed) reduced Search reply parsing and per-row map cost on earlier scan paths. The join readers added in #635, #643 and #644 were not covered.

Not established: how much of SF1 RAM latency comes from GC. On SF1 Flex, workers spent 87–90% of their time waiting on replies (`docs/sdlc/changes/tpch-flex-read-path/validation.md:62`), so the gain there may be small.

## Desired behavior

Fewer allocated bytes per row read on the streaming star/snowflake paths, for example through column-typed batches or reused row storage. Results, NULL handling and value types stay the same.

## How to verify

- Unit tests pass: `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`.
- The `Snowflake|Star` integration selection passes: `cd go && bash software-setup.sh --exec go test -count=1 -p 1 -run 'Snowflake|Star' .`.
- Measure subset Q9 before and after:
  - Capture `adbc.redis.execution_diagnostics_file` (`docs/execution-diagnostics.md:3-12`). Its allocation figures are process-wide (`:53-54`).
  - Capture a Go CPU and allocation profile.
  - Success: allocated bytes per row read and the GC share both drop, and the result still validates against DuckDB.
