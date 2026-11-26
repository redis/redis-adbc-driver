# Validation

2026-11-26 (UTC), main `844d8123`, then `ac82cfa0`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **Cause:** the slow indexes had not yet been compacted by the Flex Search disk GC.
  - Until its first GC cycle, an index keeps one disk entry per document per field, and every NUMERIC term scans all of them: about 0.6 s per term at 6.0M documents.
  - The cycle merges those entries into blocks. The same 100 terms then take 1.15 s instead of 59.7 s on SF1 `lineorder`.
- **Not the driver:** every population path starts in the per-document layout and is compacted on the same schedule. That covers the driver's FCALL notifications, plain HSETs, row writes into an existing index, and an `FT.CREATE` initial scan.
- **First cycle:** 5.9–9.4 minutes after `FT.CREATE` for all 10 indexes, including the full SF1 `lineorder`, under both 8% and 60% Search disk memory limits.
- **Fast SF1 `lineitem`:** a compacted index. After its cycle, SF1 `lineorder` has 243,804–273,632 disk keys per NUMERIC field, which matches `lineitem`'s "about 272k".
- **Not established:** why the SF1 `orders` and `lineorder` indexes on the earlier server never got their cycle; see [below](#not-established).

## Deployment

- **Server:** a dedicated fixture brought up with `go/flex-setup.sh` (2026-11-26 02:44Z):
  - Compose project `adbc-flex-layout`, ports 19443/19200.
  - Compose override: `pull_policy: never` and a 20 GiB container limit without swap.
  - Image `redislabs/redis:latest` `679d117a56fc` (Software 8.2.6-58, built 2026-11-12), the one the earlier Flex runs used, not re-pulled.
  - Database: Redis 8.8.0 with Search 8.8.8 (`80808`), bigstore v2, `search_on_bigstore`, 1 shard, 64 GiB with an 8 GiB RAM tier, `noeviction`, TLS.
  - Persistence disabled until 03:39:33, then AOF every second.
- **QPF:** the management API reported active 4x QPF (6 Search workers, `mnp`, 32 connections) after setup and after each settings change (02:46, 02:55, 03:18, 03:42).
- **Search disk settings:** memory limit / write buffer minimum / per-index buffer:
  - 8% / 2% / 3 MiB from 02:45:20, the earlier runs' values;
  - 60% / 20% / 3 MiB (the defaults) from 02:55:35;
  - 8% / 2% / 3 MiB again from 03:18:49.
  - Times are from the cluster event log. The module opened its disk store with `memory_limit_bytes=8589934592`, so the percentages apply to the RAM tier.
- **Host:** Docker VM with 16 CPUs and 31.46 GiB, running only this fixture and one client at a time.
- **Client:** `adbc-olap-release-client:20261123` (`446def603bd9`), 4 CPUs and 8 GiB without swap.
  - Route: `--network container:redis-adbc-flex` (Linux namespace route, macOS host).
  - The installed v0.0.31 wheel was pointed at each library with `REDIS_ADBC_LIBRARY`.
- **Builds:** `go/build-linux.sh arm64` in `redis-adbc-linux-builder:1.27.4-arm64` (c-shared, `-tags driverlib`, version linker flag). SHA-256 prefixes:
  - main `844d8123`: `120be769`, for the samples;
  - main `ac82cfa0`: `f3432b3a`, for SF1 `lineorder`.
  - `ac82cfa0` adds #690 and #691, which change no index creation or population code.
- **Data:** the earlier runs' SF1 DuckDB files, opened read-only.
  - The samples are the first 600,000 rows in DuckDB's scan order.
  - Loads used `adbc_ingest(mode="create")` with 4,096-row batches and `adbc.redis.flex=true`, as `run.py` does.
- **Probes:** `redis-cli` inside the server container, read-only.
  - `FT.SEARCH <index> "(@f:[k k]|…)" NOCONTENT LIMIT 0 0 TIMEOUT 600000 DIALECT 2`, with keys `1 + step·i`.
  - Times are wall clock in the container and include the client connection, about 3 ms against `FT.PROFILE`.

## Per-document layout before the first GC

600,000-row samples. Disk keys per field and inverted blocks are from `FT.INFO`.

| Index | Built by | Disk keys per field | Blocks | 100 `partkey` terms (matches) |
|---|---|---:|---:|---|
| SSB `lineorder` sample | driver | 600,000 | 1,800,000 | 5,714 / 5,719 / 5,747 ms (320) |
| TPC-H `lineitem` sample | driver | 600,000 | 3,000,000 | 5,758 / 5,669 / 5,783 ms (325) |
| `lineitem` sample under 60% | driver | 600,000 | 3,000,000 | 5,770 / 5,702 ms (325) |
| initial-scan rebuild over the `lineitem` sample's rows | `FT.CREATE` scan | 600,000 | 3,000,000 | 6,087 / 5,775 ms (325) |

- **Same layout:** both tables, both settings and the scan rebuild. The block count is documents × TAG fields: 3 TAG fields for `lineorder`, 5 for `lineitem`.
- **Per term:** one term costs 60–62 ms. `FT.PROFILE` shows a `DISK_NUMERIC` iterator: 58.4 ms, 3 reads, "Estimated number of matches 600000". That is about 97 ns per document.
- **Per-index store:** each index has its own disk store (`flash/bigstore-1/search/<index>/main`), with a column family per field.
  - NUMERIC families use `numeric_merge_operator` and `disable_auto_compactions=true`.
  - Each document's entry is a put with a 25-byte key and a 19-byte value: memtable flushes of `numeric_4` wrote 62,440 entries and 0 merge operands.
  - This matches RediSearch 8.8-rse, where `DiskIndexer_IndexDocument` commits one disk write batch per document.

## The first GC cycle compacts the index

**What the cycle did.**
- It logged "Starting full compaction on inverted index column family" for every family. The store's LOG shows a manual compaction per family: for one TAG family of the `lineitem` sample, 600,000 input records became 6,385 output records.
- `gc_stats`: 2.6 s and 158 MB collected for the `lineitem` sample; 2.9 s and 173 MB for `lineorder`.

**The samples after it.**

| Index | Disk keys per field | Blocks | 100 `partkey` terms |
|---|---:|---:|---|
| SSB `lineorder` sample | 6,383–50,822 | 19,157 | 20 / 15 ms |
| TPC-H `lineitem` sample | 6,384–50,822 (`l_comment` 525,523) | 551,062 | 21 / 16 ms |

One term costs 0.46 ms (`FT.PROFILE`; iterator 0.08 ms, against 58.4 ms before).

**SF1 `lineorder` (6,001,171 rows, driver, 8% / 2% / 3 MiB).**
- Load: rows 03:27:31, `FT.CREATE` 03:29:43, load complete 03:32:06 (256 s).
- Before the cycle: 6,001,171 disk keys per field and 18,003,513 blocks, the SF1 runs' layout.
- First cycle at 03:37:06: 16.7 s, 1.73 GB collected. Afterwards, 243,804–273,632 disk keys per NUMERIC field, 63,844–63,846 per TAG field, and 191,536 blocks.

| Union | Matches | Before the cycle | After it |
|---|---:|---|---|
| 1 `lo_partkey` term | 25 | 811 / 612 / 598 ms (`FT.PROFILE` 596 ms) | 24 / 7 / 6 ms (`FT.PROFILE` 4.3 ms) |
| 100 `lo_partkey` | 2,914 | 59.7 s | 1.30 / 1.15 / 1.15 s |
| 100 `lo_suppkey` | 299,776 | — | 5.5 / 1.6 s |
| 100 `lo_custkey` | 20,252 | — | 1.14 / 1.03 s |
| 1,000 `lo_partkey` | 29,893 | — | 11.4 / 11.3 s |
| 2,000 `lo_partkey` | 60,071 | — | 23.3 s |

- **Uncompacted:** 59.7 s for 100 terms reproduces the earlier record's 56.3–57.0 s on SF1 `lineorder`.
- **Compacted:** about 11.5 ms per term, within the fast SF1 `lineitem`'s 12–26 ms. A term still scans every remaining entry; the cycle only makes them about 22× fewer.
- **SSB:** at that cost, the 278–1,924-term demands of the queries that timed out would take about 3–22 s per evaluation.

## When the first cycle runs

Times are from the shard log: "Successfully created index" to the first "Starting full compaction".

| Index | Populated by | Settings | `FT.CREATE` | First cycle | Delay |
|---|---|---|---|---|---:|
| SSB `lineorder` sample | driver FCALL notifications | 8%, then 60% from 02:55:35 | 02:46:56.7 | 02:56:05.7 | 549 s |
| TPC-H `lineitem` sample | driver | 8%, then 60% | 02:47:30.6 | 02:56:09.6 | 519 s |
| initial-scan rebuild | `FT.CREATE` scan, done 02:56:56.7 | 8%, then 60% | 02:50:02.0 | 02:59:01.0 | 539 s |
| `lineitem` sample under 60% | driver | 60% | 02:56:53.3 | 03:06:15.4 | 562 s |
| 20k rows, A | plain HSET touches after `SKIPINITIALSCAN` | 60% | 03:06:17.1 | 03:14:35.1 | 498 s |
| 20k rows, C | row writes into an existing index | 60% | 03:06:17.6 | 03:15:03.6 | 526 s |
| 20k rows, D | touches from one FCALL per 256 keys | 60% | 03:06:18.3 | 03:15:15.3 | 537 s |
| 20k rows, A | plain HSET touches | 8% | 03:18:55.3 | 03:24:54.3 | 359 s |
| SF1 `lineorder` | driver | 8% | 03:29:43.1 | 03:37:06.1 | 443 s |
| 20k rows, A | plain HSET touches | 8%, AOF every second | 03:42:47.5 | 03:48:43.5 | 356 s |

- **8%:** the earlier runs' setting does not hold the cycle back. At 03:18, Search used 2.6 GB of index memory, far above 8% of the RAM tier (687 MB).
- **No client trigger:** creating and dropping an unrelated index at 03:10:05 did not start the 20k-row indexes' cycles. They ran 4–5 minutes later. `FT.DEBUG` is not available on Redis Software.
- **Timer:** the delay counts from `FT.CREATE`, not from the last write: the initial-scan rebuild ran its cycle 2 minutes after its scan finished.
- **Source:** in RediSearch 8.8 / 8.8-rse (`disk_gc.c`, `gc.c`, read from GitHub; not the exact server build):
  - the disk GC ticks every 30–60 s and runs once writes, deletes and updates reach `search-fork-gc-clean-threshold` (100);
  - it does not run while background work is paused, which a disk consistency window (SST replication checkpoint or fork, hot-restart save) does;
  - one thread runs all indexes' cycles (`GC_THREAD_POOL_SIZE 1`).
  - These sources do not explain the 5.9–9.4 minute first delay, which comes from the closed disk implementation's state or scheduling.

## Ruled out

- **Table or data:** the `lineitem` and `lineorder` samples have the same layout and cost before their cycle, and the same kind after.
- **The driver's population path:**
  - the deferred final index populated by FCALL notifications;
  - plain HSET notifications;
  - an existing index populated by row writes (the early append index's path, `snapshot_append_index.go`);
  - an `FT.CREATE` initial scan.
  All start with one entry per document and are compacted on the same schedule.
- **Row order and key order:** each document is its own disk batch whatever the order. The scan rebuild, in SCAN order, and the driver, in row-ID order, both start at 600,000 keys per field. Their compacted results differ only slightly.
- **The 8% / 2% Search disk settings:** cycles run on schedule under them.
- **Index size:** the 6.0M-document index compacts on schedule in 16.7 s.
- **Another index's `FT.CREATE` / `FT.DROPINDEX`:** does not start the cycle. The driver's connection probe is such a pair.
- **A plain AOF rewrite:** the rewrite at 03:39:33 logged `SAVE end mode=FULL_RDB sst=false`, without a consistency window. The 20k-row index created under AOF afterwards had its cycle on schedule (356 s).

## Driver change

None. No driver path produces the per-document layout, and no client command can compact a Flex index on Redis Software. FCALL, RESP3, pushdown and fallback behavior are unchanged, so there is no failing-first test. The SSB SF1 rerun is not required (see [intent](intent.md)).

What a Flex benchmark needs instead:
- **Before timing:** wait for, or check, each table index's first GC cycle. `FT.INFO`: `gc_stats.total_cycles` ≥ 1, and `disk_num_keys` below `num_docs` for its NUMERIC fields.
- **Provenance:** record both values.

## Not established

- **The earlier server:** why the SF1 `orders` and `lineorder` indexes on the server used by #689 and `flex-ssb-key-demand` were still uncompacted hours after loading. That server is gone, so these candidates cannot be checked:
  - a shard restart or reload before their first cycle: the GC counters live in memory and start at zero, so a reopened index gets no cycle until 100 new changes;
  - the single GC thread held by a cycle that did not finish;
  - a consistency window held open (that server used AOF; a plain AOF rewrite opened none here).
- **The first delay:** why the first cycle comes 5.9–9.4 minutes after `FT.CREATE`, and whether later cycles of a growing index keep that delay.
- **Other deployments:** other Software, Redis or Search versions, more shards, Redis Cloud.
- **SSB:** no query was run. The 3–22 s estimate above is from the probe, not a suite result.

## Fixture cost and ownership

Owned by this session: local Docker on the developer's Mac, trial license, no cloud resources, no cost. The API still reported active 4x QPF at 03:49, and the container and its Compose network were removed at 03:49:37Z (`docker compose --profile flex down`). The scratch probe indexes went with the container.
