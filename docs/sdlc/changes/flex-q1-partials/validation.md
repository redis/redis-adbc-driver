# Validation

2026-11-25; branch from main `b1efe706`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **Warm:** SF1 Flex Q1 finished under 60 s on the warm-up and all three timed runs: 32.7, then 31.4 / 34.7 / 38.0 s.
- **Cold:** with lineitem pushed out of the 8 GiB RAM tier, Q1 is not under 60 s. It timed out at 300 s on 32 workers. An uncommitted build allowing 64 and 92 workers took 256.5 s and 217.9 s.
- **Bound:** the single shard's cold flash reads limit Q1 to roughly 15–28k rows/s, so 6M rows take well over 60 s.

## Profile

**SF1 diagnostics (#646 build, warmer run):**
- **Columns:** the partitions read Q1's 6 aggregate columns plus `l_shipdate` for the filter, so no extra fields.
- **Wire:** 116 bytes read and 293 bytes written per row. Each HMGET repeats the key and 9 field names.
- **Client:** 10.5 KB allocated per row.
- **Time:** workers spent 2,317 of 2,427 worker-seconds in partition fetch.

**Subset Q1 profile** (Flex mode, Linux namespace route, 4-CPU container, `go test` profiles). About 10 KB was allocated per row, from:
- the per-row decode map,
- the per-row value slice and the tuple's row map,
- big-number decimal arithmetic for Q1's expressions,
- string-encoded partials and source values for every tuple,
- the spill buffer's later sort, string parse and combine.

| Subset Q1 | Allocated | CPU | Elapsed |
|---|---|---|---|
| main `b1efe706` | 6.12 GB | 8.39 s | 2.54 s |
| this change | 5.39 GB (−12%) | 7.02 s (−16%) | 1.98 s |

## Request bytes

Warm SF1 Q1 wrote about 290 bytes per row to read about 116. Each HMGET carries the full row key (about 110 bytes) and 9 field names. Shorter key or field encodings would change the storage format. The row-ID and tombstone fields are needed for exactness.

The remaining option was a read-only Redis Function taking a batch of row keys with the field list once. It was measured on a dedicated Redis Software 8.8.0 database: 1 shard, API-verified active 4x QPF, mnp, 32 connections. The data was 300,000 synthetic lineitem-shaped HASHes in one `{meta}` slot, 9 fields read per row, 256 rows per pipeline or call, read from a 4-CPU Linux client in the server's network namespace. The function was a scratch `no-writes` FCALL_RO library, not part of this change.

| Connections | Pipelined HMGET rows/s | Batched FCALL_RO rows/s |
|---|---|---|
| 1 | 475,856 | 291,536 |
| 4 | 863,552 | 514,742 |
| 8 | 1,061,151 | 467,348 |
| 32 | 1,124,843 | 496,121 |

- **Bytes:** the function cuts request bytes from 271 to 85 per row.
- **Speed:** it reads at roughly half the rate. Each call runs single-threaded on the shard for its whole batch.
- **Flex:** a script additionally needs its keys in RAM before running.

So it is not adopted, and it was not run on Flex: loading a function would be a write to that database.

## Tests

- **Combiner:** `TestPartialGroupCombinerMatchesSequentialCombine`. Every group equals the sequential combine, with fewer tuples handed on than rows. This is a performance change with unchanged results, so the existing exactness tests also pin its behavior.
- **Unit suite:** `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...` passes (all 7 packages).
- **Integration:** on the shared Redis Software fixture, `TestSQLFlexPartitionedAggregate`, `TestSQLPartitioned*`, the decimal tests and the combiner test pass.
- **Snapshot:** on a dedicated Redis Software 8.8.0 fixture (3 shards, API-verified active 4x QPF, mnp, 32 connections), `-run 'Snapshot.*(Flex|Partition|Aggregate|Tuple|Directory)|TestSnapshotPartitioned'` gave 45 passed, 6 skipped, 0 failed.
- `gofmt` clean; `go vet` reports nothing new.

## Subset (10% TPC-H, Linux route, 4-CPU client)

Fully fetched and validated against DuckDB:
- **Flex mode:** Q1 3.59 s (`partitioned_exact`, 10 workers), Q6 3.28 s, Q12 3.53 s, Q14 2.93 s, Q19 5.01 s.
- **RAM mode:** Q1 4.26 s, Q12 1.02 s.

The host was shared, so times are observations.

## SF1 Flex

**Setup.**
- **Deployment:** Redis Software 8.8.0 Flex, 1 shard, 8 GiB RAM tier, verified 4x QPF.
- **Client:** one 4-CPU / 8 GiB Linux client through `--network container:redis-adbc-flex`.
- **Window:** exclusive and read-only, 2026-11-25 04:56–05:20Z.
- **Runs:** each run was a fresh process, fully fetched and validated against the SF1 DuckDB source.
- **Connections:** server client connections were sampled every 3 s from the management API. The database's `max_connections` is 0 (no limit).

**Builds.**
- **Committed:** library `v0.0.31-flexwip.66be15a2`, this change, with the 32-worker Flex default.
- **Uncommitted experiment:** `v0.0.31-flexexp.w128` is this change with `query_workers` validation relaxed to 128, run with the URI's `pool_size` raised. Its numbers are not credited to a committed build.

| Run | Build | Workers | Seconds | Rows | Peak RSS | Max server conns |
|---|---|---|---|---|---|---|
| warm, single | committed | 32 | 34.5 | 6,001,215 | 0.2 GiB | 44 |
| warm, warm-up + 3 timed (60 s deadline) | committed | 32 | 32.7, then **31.4 / 34.7 / 38.0** | 6,001,215 | 0.2 GiB | 43 |
| warm, same state | #646 (`b1efe706`) | 32 | 51.9 | 6,001,215 | 2.3 GiB | 45 |
| cold | committed | 32 | timeout at 300 s | 4,621,056 read | 0.2 GiB | 46 |
| cold | uncommitted experiment | 64 | 256.5 | 6,001,215 | 0.2 GiB | 78 |
| cold | uncommitted experiment | 92 | 217.9 | 6,001,215 | 0.2 GiB | 102 |

**Warm runs.** The committed single run read 694,625,520 bytes and wrote 1,759,994,457 on the connection, and allocated 9.4 KB per row. Its workers waited about 88% of the time.

**Cold runs.** Each cold run followed read-only HMGET reads of all of orders, partsupp, customer and part, which push lineitem out of the RAM tier. Those reads were themselves partly warm before the 92-worker run, so the cold states differ and the comparisons are approximate. Cold read rates were:
- about 15k rows/s on 32 workers,
- about 23k on 64,
- about 28k on 92.

More workers help cold reads somewhat, but nowhere near 60 s, and they add server connections. The default and the 32-worker cap are not changed.

## Not established

- **A controlled cold series:** a cold-cache series on a quiescent RAM tier.
- **Larger deployments:** whether a larger RAM tier or more shards would hold Q1 under 60 s cold. That is a deployment change, outside this work.
- **Review:** human review is pending.
