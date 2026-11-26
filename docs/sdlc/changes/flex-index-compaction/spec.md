# Spec

2026-11-26; see [intent](intent.md), [plan](plan.md), [validation](validation.md). Applies to Redis Software 8.2.6 with Redis 8.8.0 Flex databases (`search_on_bigstore`, Search module 8.8.8), the deployment the SF1 Flex runs used. Other versions were not checked.

## Finding

**Flex Search indexes are written one document at a time and compacted later by the disk GC.**
- Each indexed document commits its own disk write batch (`DiskIndexer_IndexDocument` in RediSearch 8.8-rse). So every NUMERIC and TAG field gets one disk entry per document, and each TAG term one inverted block per document.
- A NUMERIC term is a `DISK_NUMERIC` iterator that walks every entry of its field. `FT.PROFILE` reports the index's document count as its estimated matches, whatever the term matches. That costs about 95–100 ns per indexed document per term, which is the SF1 runs' 91–93 ns.
- The disk GC's first cycle runs a full manual compaction of every column family. It merges those entries into blocks: for 600,000 documents, 6,400–51,000 entries per NUMERIC field, and for 6.0M about 244,000–274,000. A point term then costs about 0.5 ms at 600,000 documents and 6–11 ms at 6.0M, against 60 ms and 0.6 s before.

**When the first GC runs.** The GC ticks every 30–60 s and runs once at least 100 writes have accumulated and background work is not paused. Here the first cycle came 5.9–9.4 minutes after `FT.CREATE` for every index tried. Clients cannot start it: `FT.DEBUG GC_FORCEINVOKE` is not available on Redis Software.

## What this means for the driver

- **Not a driver path.** Every way of populating an index starts in the per-document layout and is compacted on the same schedule. That covers the driver's deferred FCALL notifications, plain HSETs, row writes into an existing index, and an `FT.CREATE` initial scan. The early append index (`snapshot_append_index.go`) is a row-write-into-existing-index path. Row and key order do not matter: each document is its own batch either way.
- **No driver change.** Nothing the driver controls changes the layout or the GC schedule, so FCALL, RESP3, pushdown and fallback behavior stay as they are.
- **Benchmarks.** A Flex index timed before its first GC cycle measures the uncompacted cost. `FT.INFO` shows the state: `gc_stats.total_cycles` is 0, and every field's `disk_num_keys` equals `num_docs`. Flex timing evidence should record both, and should wait for or confirm a completed cycle before timing.

## Not established

- Why the SF1 `orders` and `lineorder` indexes on the server used by #689 and `flex-ssb-key-demand` were still uncompacted hours after loading. See [validation](validation.md#not-established).
- The GC behavior on other Software, Redis or Search versions, on more shards, and on Redis Cloud.
