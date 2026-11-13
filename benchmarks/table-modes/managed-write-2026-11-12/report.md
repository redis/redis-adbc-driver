# Managed write command attribution

November 12, 2026. [Samples](measurements.json), [summary](summary.json),
[change records](../../../docs/sdlc/changes/managed-write-publication/intent.md).

The change reduces script payloads. It does **not establish a write-throughput
improvement** on this local Software workload.

## Workload and environment

Persistent Go connection, 10,000 generated rows x 105 nullable BIGINT columns,
including exact values above 2^53. Separate 1K append calls and the existing
10K-call option; automatic compaction enabled. Every trial fetches and validates
all values, NULLs and multiplicities outside timing, with no caller retries.
Foreground hooks exclude the background worker's independent pool.

Redis Software 8.2.6-25 / Redis 8.8.0 / Search 8.8.8; one 1-GiB RAM shard,
noeviction, active verified 4x QPF, six Search workers, mnp/32 connections,
certificate-verified TLS/RESP3. Go 1.27.3, Apple M5 Max / 36 GiB; Docker has
18 CPUs and about 23.7 GiB, with no task-specific container limit. Shared
workstation; the task's tests and profiles ran separately from timed trials.

## Attribution and change

The initial 4096 x 64 SQL diagnostic took 480 ms: row batches accounted for
159 ms of client-observed time and index notifications 134 ms. These timings
include transport and server execution, not exclusive server CPU. The wider
CPU profile sampled 5.70 CPU seconds over 34.70 wall seconds including setup
and validation; transport/TLS/system calls dominated sampled work. A separate
1-MiB pipeline-buffer control did not show a dependable throughput improvement.

Standalone row writes, final index notifications and atomic compaction copies
now send the existing scripts' 40-byte digests. A definite native cache miss
or top-level EVALSHA denial permits one identical full-body attempt. Lost,
wrapped, runtime and key-permission errors do not select a fallback. Existing
row/compaction outcomes still resolve uncertain replies. Pipeline scripts, Lua
logic, storage format, row limits and publication proofs are unchanged.

For 10K rows, the two foreground batch scripts previously sent **487,440
script-header bytes**. Warm cached calls send **3,200 bytes**, 99.3% less. An
earlier confirmation trial includes both cold misses and full-body fallbacks:
15,386 bytes, 96.8% less. This counts only the body/digest argument, excluding
RESP framing, keys, row values, other commands and background traffic. It is
not a 99.3% reduction in total network bytes.

## Timing and limits

Final confirmation: three samples per case, uncached first and cached second.
The cached run starts warm because the uncached run already loads the bodies.
An earlier confirmation uses the opposite comparison order.
The uncached control uses the same final source with only the three standalone
call sites restored to full-body execution.

| Append call size | Uncached median | Cached median |
|---|---:|---:|
| 1,000 rows | 7.869 s | 8.050 s |
| 10,000 rows | 1.756 s | 1.773 s |

Final cached medians are 2.3% and 1.0% slower. The preceding reverse-order
confirmation gave a 1.6% improvement at 1K and a 0.8% slowdown at 10K. These
small, overlapping distributions on a shared workstation do not demonstrate
a throughput benefit. All 42 completed timing trials are retained with distinct
labels, including the buffer control, a pilot with an extra temporary command
slice copy, and the earlier copying-observer confirmation. The final observer
classifies digest calls without copying wide command arguments. No samples were
dropped.
The final executor takes ownership of the fresh command slice and copies only
on refusal. Timing does not justify a latency or throughput claim.

These Go diagnostics use the Go Arrow allocator. They are not a native C-ABI
benchmark or a four-writer comparison. No 100M ClickBench, Flex or unmanaged
benchmark was run. Source hashes and comparison order are in the samples.
Raw profiles, logs, credentials and infrastructure identifiers remain private.

The larger remaining opportunity is reducing publication/indexing and
compaction work itself. Cached scripts leave that work unchanged.

## Reproduction

On an owned disposable Software fixture initialized and verified by
`make -C go redis-up`, source `go/build/test.env`, then run from `go/`:

```sh
REDIS_SNAPSHOT_URI="$REDIS_URI" REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed \
REDIS_MANAGED_WRITE_PROFILE=true go test -count=1 -v -p 1 \
  -run '^TestManagedWritePublicationProfile$' -timeout=6m .
```

The opt-in test clears this selected database and its Search indexes between
subtests. Its logs contain only command categories/counts/times/header sizes.
Use the same final profiling helper for both variants; the uncached control
changes the three standalone batch call sites from `runScript` to their previous
`once(ctx, client, args...)` form. Retain complete runs, source hashes and all
validation outcomes separately. CPU profiling is a separate diagnostic.
