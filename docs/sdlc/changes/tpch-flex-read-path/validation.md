# Validation

2026-11-24; branch rebased on `12f46745` (main including #640). See [intent](intent.md), [spec](spec.md), [plan](plan.md). Status: SF1 Flex measured; Q3/Q5/Q7/Q8/Q9 do not meet the 60-second deadline on this deployment.

## Tests

- **Recursion:** `TestSnowflakeLargeBranchStreamsRecursively` admits the Q5 orders branch as its own snowflake (fact `orders.`) and returns the eager join's rows. Below the minimum fact size, the eager join is kept.
- **Directory round trips:** `TestStarChunkRangeSeekReadsOneDirectoryPage` reads full, short, two-page and tail ranges with one `ZLEXCOUNT` and the minimum number of `ZRANGE` pages. Copied onto the previous `snapshot_delta.go`, it fails: a full 256-row range read 2 pages.
- **Mocks:** directory mocks that encoded the separate seek now also answer the first page's `ZRANGE … BYLEX LIMIT` (`snapshot_directory_mock_test.go`). The panic-lifecycle mock gates the peer chunk's pipelined first page as it gated the rank page; its lifecycle assertions are unchanged.
- **Flex worker defaults:** `TestFlexQueryWorkerDefaults` covers Flex with no worker options (32 and 32), with explicit workers, with explicit `auto`, and with an explicit budget, plus a RAM database (unchanged).

Results:
- `gofmt` clean.
- Unit-only `env -u REDIS_URI go test -count=1 -p 1 ./...` passes on the rebased tree (all 7 packages).
- On the shared Redis Software fixture (8.8.0, 1 shard, verified active 4x QPF, TLS), `-run 'Snowflake|Partition|ManagedCandidate|SharedScan|DirectoryAggregate|CompactionRead|Star|Deferred|Snapshot.*Range|Cleanup' -skip 'LiveHash|Retained'`: 170 passed, 39 skipped, 2 failed.
  - The skips are snapshot suites that need a dedicated `REDIS_SNAPSHOT_URI` database. They were not run.
  - **`TestPendingCleanupAggregateAndPartitionInputs`** fails in its retained-store cleanup: a SCAN with `COUNT 100` hits a 10-second deadline (`partition_retained_store_integration_test.go:73`) on a fixture holding millions of subset keys.
  - **`TestPartitionManagedCopyOwnership/application_index`** fails with `it_partition_app_index is still indexing`.
  - Both fail identically on the pre-change tree (`adcc3b0e` without the seek change), so they are fixture conditions here, not regressions.
- **Excluded suites:** an earlier run including the live-HASH and retained-partition suites failed on preconditions:
  - Retained quotas need the `partition_control_persistence=guaranteed` declaration, which these runs didn't set.
  - The live tables hit `live table already registered or capacity reached`.
  - Some cleanup steps hit the same SCAN deadline.

  Those suites need a dedicated fixture and were not rerun.

## Subset (Linux namespace route, 4-CPU / 8 GiB client)

The 10% TPC-H subset on the local RAM fixture, through `--network container:` with the same client image and limits as the SF1 runs. Fully fetched and validated against DuckDB.

- **Uncontended baseline:** subset Q9 took 12.8 s before the seek change (Q3 4.8 s, Q5 4.2 s).
- **Q9 diagnostics:** 4 workers (a fresh process starts the tuner at GOMAXPROCS), 1.78M Redis commands, 38.5 worker-seconds of round trips and 6.0 GB allocated.
- **Raw throughput:** a scratch read-only HMGET probe on the same route read 316k rows/s on one connection and about 750k rows/s on eight.
- **CPU profile:** GC took 40-50% of samples. `Value` is 152 bytes, so row maps dominate allocation (about 3.4 KB per row read). This change doesn't address that.
- **Load:** later runs shared the 16-CPU Docker VM with other sessions' containers, and the same Q9 took 15-34 s. The seek change cut commands from 1,780,301 to 1,773,890, but interleaved pairs under that load could not separate its time effect. These are labeled observations, not speedups.

## SF1 Flex

**Deployment.** `redis-adbc-flex`: Redis Software 8.8.0 Flex (bigstore v2, `search_on_bigstore`), 1 shard, 8 GiB RAM tier of 64 GiB, noeviction, AOF every second.
- At the window's start, the management API showed status active, QPF active with scaling factor 4, mnp routing and 32 connections.
- The API reply doesn't expose Search workers, and `CONFIG GET`/`FT.CONFIG` don't return them. The coordinating session's setup record shows 6.
- About 6.7 GB of RAM and 6.1 GB of flash were in use, with 7.4M objects in RAM and 8.7M on flash. The SF1 dataset doesn't fit the RAM tier.

**Client and access.** One client container at a time (`adbc-olap-release-client:20261123`, 4 CPUs, 8 GiB, `--network container:redis-adbc-flex`, Linux namespace route, macOS host), 2026-11-25 00:50–01:03Z, exclusive and read-only. The library was built from this branch rebased on `12f46745` (version `v0.0.31-flexwip.5cf1995c`, 16-worker default at the time).

**HASH read probe.** A scratch read-only probe (not committed) ran pipelined `HMGET` of six lineitem fields, 256 per pipeline, at fixed concurrency.

| Rows | 1 conn | 4 | 8 | 16 | 32 | 64 | 128 |
|---|---|---|---|---|---|---|---|
| Just read (RAM-resident) | 93k/s | 231k/s | 208k/s | 151k/s | 177k/s | 236k/s | — |
| Cold, fresh ranges | — | 15k/s | — | 13–18k/s | 45k/s | 50–96k/s | 72–111k/s |

The first pass over the measured range ran at 19k rows/s with 16 connections. Each cold cell is one 100,000-row range, and the ranges differ, so cold rates are noisy. They rise with concurrency well beyond the driver's 32-worker maximum.

**Q9** (`tpch_tool.py one`, 300-second profiling deadline, validated against the SF1 DuckDB source):

| Workers | Result | Rows read | Redis commands | Round-trip worker-seconds | Allocated | Peak RSS |
|---|---|---|---|---|---|---|
| 16 (then default) | timeout at 300 s | 7,440,329 | 7,502,465 | 4,306 | 26.9 GB | 4.8 GiB |
| 32 (`query_workers`/`query_worker_budget` 32) | **266.4 s, valid (175 rows)** | 8,511,240 | 8,577,930 | 7,343 | 32.1 GB | 5.2 GiB |

The plan was the parallel streaming snowflake join, with 23,443 fact chunks and 3 parallel dimensions. The workers spent about 87–90% of their worker time waiting on replies, so the shard's flash reads are the bound, not the client. The default was raised to 32 from these runs.

**Q3/Q5/Q7/Q8** weren't run, to keep the shared window short. Each reads all 6,001,215 lineitem rows plus orders through the same readers:
- Q7's fact filter has about 139k candidates, just over the 131,072-row Flex candidate budget.
- Q8's 1,333 part keys exceed the 1,000-term union pushdown.

At the best observed cold rate (about 100k rows/s), 7.5–8.5M rows take 75–85 s. **None of the five queries can meet 60 s on this deployment through these read-path changes.** That would take fewer row reads (Q7/Q8 plan changes), much higher Flex read concurrency, or a deployment whose RAM tier holds the working set or that has more shards.

## Not established

- **SF1 timings:** Q3/Q5/Q7/Q8 at SF1 weren't timed. Q9 was not run in the harness's warm-up plus three timed runs (one validated run at 32 workers).
- **The 32-worker default build:** it wasn't run on Flex itself; the measured 32-worker run used explicit options on the 16-worker build, which select the same fixed budget.
- **Concurrency above 32:** the driver caps workers at 32. Higher concurrency was measured only with the probe.
- **Before/after comparison:** there's no SF1 Flex comparison against main beyond the coordinating session's earlier 60-second timeouts.
- **Review:** human review is pending.
