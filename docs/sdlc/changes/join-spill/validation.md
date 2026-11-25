# Validation

2026-11-25; base `0b97f07e` (main). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Environment: isolated Compose project on Redis Software 8.8.0, one shard, active verified QPF
scaling factor 4, `noeviction`, TLS, 4.5 GiB database (`software-setup.sh`, `SOFTWARE_SHARDS=1`).
Client: macOS ARM64 host (10 CPUs, Go 1.27.4) through a Docker published port; other sessions'
fixtures shared the Docker VM.

## Unit tests (in-memory snowflake fixture, no Redis)

- `TestSnowflakeBranchSpill` (new). The Q3 / Q5 / Q7 / Q9 / materialized-row shapes each run at
  branch caps 8, 16, 32, 64, 96, 128 and 200, with 1, 4 and 16 partitions (21 settings).
  - Every run's rows equal the eager join's as a multiset.
  - Settings that completed a spilled join (`joinSpillStats.joined`): q3 2, q5 8, q7 5, q9 1,
    rows 6. The others fell back, because a second dimension or a partition exceeded the cap.
  - No spill directory remained after any run.
- `TestSnowflakeBranchSpillFallsBackBeforeFacts` (new): with one partition, and with
  `testNoJoinSpill`, the Q5 shape at cap 3 returns `handled=false` without visiting a row.
- The existing `TestSnowflakeBranchCapFallsBackBeforeFacts` still passes: at cap 3 the fixture
  overflows two dimensions, so it falls back as before.

## Redis integration

`TestSQLSnowflakeJoinsManaged` gains two subtests on its 6,000-line fixture:
- `cap_fallback`: with `testNoJoinSpill`, cap 64 and the parallel dimension reader, the `dims`
  query must fall back and still return the eager rows.
  - Before the fix in `parallelTableRows` it fails: `I/O: [redis] FT.SEARCH failed: set tcp
    127.0.0.1:…: use of closed network connection`.
  - Before the flag-based stop, the spilled re-read failed the same way, with `FT.AGGREGATE
    failed`.
  - After the fix it passes: 2,966 rows equal, and the eager join records `lookup_join_order`.
- `spilled`: every query (q5, q3, q7, dims, rows) at caps 64, 400 and 1,500 with 8 partitions
  equals the eager baseline. `dims`, `rows` and `q7` completed spilled joins, including the
  parallel dimension reader's serial re-read.

Both subtests and the original five pass.

## Subset Q9 memory

Fixture: the 10% TPC-H subset (1.91M rows) loaded through the driver. Q9 retains about 0.96M
branch rows there, below the default 4M cap.

A scratch `go test -overlay` runner (not committed) ran the query twice per configuration in a
fresh test process, reading the full Arrow stream. Peak live heap is the maximum of
`/memory/classes/heap/objects:bytes`, sampled every 5 ms from before execute until the stream
ended. Every run returned 175 rows with the same digest of the sorted rows.

| configuration | peak live heap | time | allocated | path |
|---|---:|---:|---:|---|
| default cap (4M) | 1,658 / 1,647 MB | 17.2 / 16.7 s | 4.9 GB | in-memory streamed snowflake |
| cap 600,000, spilling off (`testNoJoinSpill`), i.e. main's fallback | **7,268 / 5,192 MB** | 50.0 / 41.6 s | 12.6 GB | eager join fallback |
| cap 600,000 | **875 / 880 MB** | 51.0 / 41.6 s | 9.3 GB | one dimension spilled to 64 partitions |
| cap 262,144 | 471 / 498 MB | 23.5 / 21.6 s | 8.5 GB | one dimension spilled to 64 partitions |

The default subset Q9 was validated against DuckDB in the #652 A/B, and every configuration here
returns identical rows.

Over the cap, spilling replaces the eager join's 5–7 GB with under 0.9 GB, in the same time or
less. It is slower than the in-memory streamed join, because in spill mode the fact is read by
the serial reader and every fact row passes through a file.

## Other checks

- Unit-only `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages pass.
- `gofmt -l` on the changed files: clean.
- `TestSQLSortSpill` passes on the shared `spillRowCodec` (in the selection below).
- Integration selection `-run 'Star|Join|Q19|TPCDS|Deferred|Snowflake|Union|Natural|Lateral|SemiJoin|Subquer|Grouped|CTE|Common|FactDemand|OwnedFact|TestSQLPartitioned|Decimal|Sort|Order'`
  (233 top-level tests) through `software-setup.sh --exec`: `ok … 151.923s`.
- With `-race`: `TestSQLSnowflakeJoinsManaged` (including the parallel reader's cap stop),
  `TestSQLSortSpill` and `TestSnowflakeBranchSpill*`: pass, no race reports.

## Not established

- Only one dimension spills. A second over-cap dimension, or one oversized partition (skewed
  keys), still falls back to the eager join. Nothing repartitions recursively.
- In spill mode the fact is read serially and the partitions are joined one at a time; the
  parallel fact readers are not used.
- Star (non-snowflake) shapes, the eager join itself, and consumers that retain the whole join
  output (`snowflakeJoinRows` in `scanMem`) are unchanged.
- The partition count (64) and the cap are internal, with no user option.
- SF1 and multi-shard runs were not measured locally.
- Human review is pending.
