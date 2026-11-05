# Software Flex follow-up performance and resources

Date: 2026-11-05. [PR #331](https://github.com/redis/redis-adbc-driver/pull/331), follow-up to [#330](https://github.com/redis/redis-adbc-driver/issues/330). Before driver: `6410c8e`, maximum Flex page 1,000. After driver: `a121842`, maximum 10,000. The opt-in profiler source and request timestamps are included in those changes; the added cold profiler is inactive during warm trials. This report supplements the [original comparison](../README.md).

Larger bounded pages improved broad-query latency by roughly 1.8–2.8x in this fixture, with higher heap peaks for several narrow workloads. The stopgap remains slower than normal RAM execution for broad scans, ordering and aggregates. It remains a capability/index-memory tradeoff, with no general speedup claim.

## Matched warm comparison

- Same 50,000 rows and 15 columns as the original report: id, group, nullable numeric score and twelve unindexed strings with a 120-byte suffix. Every returned field was validated against formulas and the entire Arrow stream consumed.
- Both targets were single-shard Redis Software 8.2.6-25, Redis 8.8.0, Search 8.8.8, Linux aarch64, authenticated TLS/RESP3, no replication, noeviction and verified active 4x QPF (6 Search workers, mnp routing, 32 connections).
- RAM: 1 GiB. Flex: BigStore v2, 1 GiB total / 256 MiB RAM. The warm dataset fits that RAM tier. Flash is a container directory, not an independently provisioned SSD service.
- One worker/budget=1, Go 1.27.3, GOMAXPROCS=18. Docker exposed 18 CPUs and 25,421,033,472 bytes of VM memory. Shared macOS ARM host; no CPU pinning.
- One warmup plus five measured repetitions, rotating mode order within each workload. RAM and Flex profiles ran sequentially, without other validation suites. Before and after are separate runs; fresh reserved prefixes and background storage work can affect bytes/RSS. Default RAM controls also vary between runs.
- Timings include execute, full fetch and the oracle. They are not server-only times. Five repetitions do not establish a production p95.

All latency cells are **before → after median milliseconds**.

| Workload | RAM default | RAM stopgap | Flex stopgap |
|---|---:|---:|---:|
| selective | 8.99 → 19.01 | 7.62 → 8.01 | 12.87 → 19.22 |
| broad | 306.30 → 328.48 | 2394.98 → 988.84 | 4537.68 → 2166.92 |
| wide | 682.61 → 717.54 | 2671.26 → 1281.56 | 4927.47 → 2720.38 |
| topk | 21.27 → 24.36 | 2520.74 → 915.76 | 4618.67 → 2122.88 |
| sort | 381.24 → 419.96 | 2617.75 → 987.28 | 4680.57 → 2167.37 |
| scalar | 270.16 → 223.38 | 2599.97 → 1011.33 | 4561.17 → 2245.87 |
| grouped | 268.75 → 282.77 | 2571.87 → 1017.17 | 4572.72 → 2427.98 |

Selective is the final 100 IDs; broad projects id/n; wide projects id plus twelve strings. Top-K orders nullable n/id and returns 100 rows. Sort orders the full set. Scalar and grouped aggregates match the original report. The selective Flex case regressed by about 6 ms; no improvement is claimed for it.

The raw factorial comparison still isolates both numeric schemas and both readers on RAM, with identical exact HASH fetching and no server sorting/reducers:

| Broad raw scan | Before, ms | After, ms | Redis commands, before → after |
|---|---:|---:|---:|
| sortable/cursor | 780.07 | 752.68 | 50006 → 50006 |
| sortable/bounded | 2546.89 | 969.65 | 50129 → 50017 |
| plain/cursor | 820.99 | 788.95 | 50006 → 50006 |
| plain/bounded | 2581.49 | 951.40 | 50129 → 50017 |

For this dense 50,000-ID raw scan, adaptive Search requests fall from 127 to 15; the 50,000 HASH commands remain. Larger discovery pages can transfer more discarded keys even with fewer requests, so this is not a blanket bytes-reduction claim. Commands are not round trips: HASH commands are pipelined. Width-based sizing and lower server caps still reduce page size. Full/incomplete replies still split before emission; no offsets or partial-page success were introduced.

The existing exact client top-K heap remains in use. No new server sort/limit pushdown is enabled. General sort spilling is unavailable in this driver and was skipped under the requestor's instruction to skip unavailable features; it is not claimed as validated.

## Warm resource measurements

Cells are before → after medians. Client CPU/heap include result verification. Server CPU/RSS sum redis-server and dmcproxy processes; background work is included. RSS peaks are sampled every 20 ms inside the container and correlated with query timestamps. Very short queries may have no in-window RSS sample; raw samples record that explicitly.

| Target / workload | Client CPU, s | Server CPU, s | Client heap peak, MiB | RESP read+write, MiB | Server RSS peak, MiB |
|---|---:|---:|---:|---:|---:|
| ram/broad | 0.36 → 0.21 | 0.20 → 0.17 | 19.02 → 25.11 | 14.04 → 14.73 | 492.78 → 506.79 |
| ram/wide | 0.58 → 0.50 | 0.42 → 0.41 | 304.98 → 293.00 | 94.48 → 95.17 | 682.99 → 730.66 |
| ram/topk | 0.47 → 0.27 | 0.21 → 0.18 | 10.07 → 22.33 | 14.04 → 14.73 | 486.05 → 569.53 |
| flex/broad | 0.45 → 0.32 | 6.05 → 5.48 | 18.28 → 25.37 | 14.04 → 14.73 | 306.86 → 336.20 |
| flex/wide | 0.74 → 0.66 | 6.37 → 5.70 | 308.77 → 307.17 | 94.48 → 95.17 | 301.13 → 329.60 |
| flex/topk | 0.52 → 0.36 | 6.02 → 5.40 | 9.63 → 19.97 | 14.04 → 14.73 | 301.48 → 326.93 |

Heap is sampled at 1 ms. These are sampled peaks, not guaranteed maxima or driver-only allocations. Process lifetime RSS includes fixture setup and is retained only in raw samples, not used as a query-memory comparison. Server RSS/cache baselines carry allocator history across runs. The configured Flex RAM tier is not a cap on process RSS or OS page cache.

## Flash-sized data and concurrent readers

A separate Software Flex test loaded **150,000 rows × 4,096-byte deterministic SHA256-derived payloads = 614,400,000 payload bytes (585.94 MiB)**, plus IDs, HASH overhead and the index. Payload blocks vary by row/block. Only n is indexed; payload is NOINDEX. Fixture loading is excluded from query timings. The same single-shard 1 GiB / 256 MiB Flex configuration and verified QPF were used.

Every reader fully fetched and checked every ID and every payload byte against the generated oracle. Three trials at 1, 2 and 4 independent connections were run with default batches, then repeated with the existing `adbc.redis.stream_batch_bytes=8388608` option. Across both variants, 18 cases validated 6.3 million returned rows. Row/byte validation and its CPU/allocations are included in the measurements.

| Arrow batch configuration | Readers | Median wall time, s | Aggregate rows/s | Sampled client heap peak, MiB |
|---|---:|---:|---:|---:|
| default | 1 | 10.97 | 13,676 | 863.6 |
| default | 2 | 14.10 | 21,279 | 1721.6 |
| default | 4 | 19.47 | 30,815 | 3431.4 |
| 8m-byte-budget | 1 | 10.17 | 14,745 | 188.3 |
| 8m-byte-budget | 2 | 11.00 | 27,267 | 358.0 |
| 8m-byte-budget | 4 | 13.50 | 44,443 | 681.1 |

Defaults allow 65,536 rows per Arrow batch, explaining the large footprint of 4 KiB values. The byte budget lowers Arrow output buffering; it is not an 8 MiB total-query memory cap. Search/HASH pages, pending rows, previous/current Arrow buffers, oversized individual values and the oracle consume additional memory. No default batch setting was changed by this PR.

Sanitized database statistics sampled during the byte-budget run confirmed **250–260 MB used RAM and 466–474 MB used BigStore**, with positive `big_fetch_flash`, `bigstore_io_reads` and `bigstore_io_read_bytes`. These are the server's reported interval metrics, not measured per-document SSD amplification. Flash and resident object counts can overlap.

This is a flash-dominant working set and a first scan after loading followed by repeated tiered scans. Host-wide page caches were not flushed. All measured per-query Linux process block-read deltas were zero: backing-store fetches were visible in database counters, but physical SSD reads/latency were not established on this shared, cached Docker host. Do not treat the result as cold-SSD sizing evidence.

## Artifacts and reproduction

- [360 warm measured samples](warm-samples.jsonl), [72 warm summary groups](warm-summary.json), and per-stage metadata/index JSON in before/ and after/.
- [18 cold/concurrent samples](cold-concurrent-samples.jsonl), [summary](cold-concurrent-summary.json), [default metadata](cold-default-metadata.json), [byte-budget metadata](cold-8m-byte-budget-metadata.json), [42 numeric tier snapshots](storage-tier-samples.jsonl).
- [Validation and remaining restrictions](../../../../docs/sdlc/changes/324-330-flex-followups/validation.md).

From go/, select a caller-owned, verified Software endpoint and container. The profiler never records the container name in its numeric timeline. Keep raw logs private.

```sh
# REDIS_URI and SSL_CERT_FILE come from the owned fixture's test environment.
export REDIS_FLEX_BENCH_DIR=/private/tmp/flex-profile-new
export REDIS_FLEX_BENCH_TARGET=flex  # or ram for the factorial baseline
export REDIS_FLEX_BENCH_CONTAINER='<owned-software-container>'
python3 validation/profile_flex.py

# Separate large-data profile; use a fresh output directory.
export REDIS_FLEX_BENCH_DIR=/private/tmp/flex-tier-profile-new
export REDIS_FLEX_COLD_PROFILE_DIR="$REDIS_FLEX_BENCH_DIR"
export REDIS_FLEX_BENCH_TEST='^TestFlexColdResourceProfile$'
export REDIS_FLEX_COLD_BATCH_BYTES=8388608  # omit for default batching
python3 validation/profile_flex.py
```

The 20 ms server sampler is instrumentation overhead present in both warm stages. It excludes startup/teardown from query timing but can affect the shared host. Timing intervals can be joined to timeline samples by StartedUnixNano/FinishedUnixNano. Published data contains metrics and controlled fixture metadata; credentials, endpoints, infrastructure identifiers and raw operational logs are excluded.
