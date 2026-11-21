# Concurrent wide compaction measurement: issue #565 closeout

Measured 2026-11-20 local time. Issue [#565](https://github.com/redis/redis-adbc-driver/issues/565).
[Shared provenance](method.json) · [Six observations](compaction-concurrent-controls.json) ·
[Backlog samples](compaction-backlog.csv) · [Earlier isolated controls](../../../docs/sdlc/changes/issue-565-compaction/evidence.md).

## Workload and correctness

Each case loads 262,144 canonical mixed-type rows ×105 columns in three disjoint
waves of four independent long Arrow appends: twelve appends total, with wave
sizes 87,381 /87,381 /87,382. Source and canonical fingerprints match the earlier
C-shared controls: 65,536 rows from each of the first four original source groups,
rewritten into 4K-row groups. This is representative wide data, not the complete
ClickBench input or a cold SSD experiment.

Three observations per baseline/current version, alternating baseline/current,
current/baseline, baseline/current. Every trial uses a fresh client process and
fresh owned namespace on the same verified Software Flex/QPF4 fixture. Source,
105 NOINDEX columns, TLS/RESP3, client Go/Python limits and common harness are
identical. Normal background maintenance remains enabled, with the original
four-segment trigger and eight-segment admission limit. No caller replay,
foreground compaction or threshold change was used.

All six cases pass exact native row-ID membership, physical Arrow schema,
independent typed point checks, and a two-way DuckDB EXCEPT ALL comparison of
all 105 values and row multiplicities. Source rows yielded and acknowledged
rows are separately recorded. Source positions are never equated to logical IDs.
After successful DROP, owned graph directories and data/metadata/compaction
queues are verified drained before the next trial. Index cleanup follows the
tested driver ownership protocol; no independent orphan-index audit is claimed. No resource alarm or process-incarnation change occurred.

## Acknowledgment and maintenance

| Driver | Median final ACK s | ACK range s, n3 | Median post-ACK compaction-quiet s | Median client load CPU s | Maximum client peak RSS through ACK MiB |
| --- | ---: | ---: | ---: | ---: | ---: |
| baseline | 141.176 | 141.079–141.432 | 125.245 | 30.899 | 1072.3 |
| current | 93.606 | 92.432–94.159 | 101.187 | 26.042 | 1004.1 |

All six cases sample active copying while ingestion is still running. Each
reaches 524,288 physical directory members (2× the logical row count), a peak
candidate queue of one and data-GC queue of five. Wave three
accounts for most of the final ACK interval, consistent with maintenance/admission
contention; there is no dedicated admission-wait timer, so its entire duration
is not attributed to admission alone. Per-wave times, overlap samples, queue
peaks, physical rows and resource scopes are included in the JSON/CSV.

The baseline/current comparison includes both single-pass indexing (#564) and
bounded compaction bursts (#565). It is a measured whole-driver comparison;
it does not isolate the contribution of #565. The earlier nine validated
105-column isolated controls vary only one/two/four 64-row atomic batches per
maintenance step: four batches reduce median maintenance 635.201→556.277ms here,
with all 64 atomic commits and their outcome proofs retained. The one-MiB target,
single-row ceiling and protocol-17 atomic bound are unchanged.

## Resource and phase attribution

Client CPU includes Python, embedded Go and observation. RSS and Arrow peaks are
separate; GC marked-live heap spans the whole case and is not RSS-minus-Arrow.
The JSON includes direct Linux data-Redis process/main-thread CPU and whole
Software-container physical I/O in inner/enclosing sampled windows, separately
for loading and post-ACK maintenance. One-second server counters and quarter-
second client samples bound windows; they are not exact per-command integrals.
PID plus start ticks establish process identity. Internal control Redis is
excluded from data-process CPU, but container I/O includes control/admin work.
Per-process I/O is permission-denied; all-zero INFO CPU is unavailable. Search-
only thread CPU is not guessed from thread names.

Medians of the inner/enclosing windows across n3 observations follow. Ranges
are sampling brackets, not confidence intervals; cgroup memory is a sampled
maximum across the three trials and includes anonymous/file/kernel components.

| Driver | Window | Data Redis CPU seconds | Container read GiB | Container write GiB | Maximum server cgroup GiB |
| --- | --- | ---: | ---: | ---: | ---: |
| baseline | load | 449.14–453.64 | 8.261–8.295 | 2.592–2.671 | 4.03 |
| baseline | maintenance | 329.43–339.31 | 7.039–7.167 | 2.121–2.125 | 4.03 |
| current | load | 298.93–302.23 | 6.455–6.491 | 2.279–2.280 | 4.26 |
| current | maintenance | 339.54–348.23 | 6.730–6.882 | 1.504–1.504 | 4.48 |

Concurrent phase samples observed copying; short index/install transitions can
fall between one-second observations. Exact concurrent index/install CPU/I/O is
unavailable at this sampling resolution. Earlier isolated hooks time copy,
index and install separately (copy medians 615.162/574.899/544.788ms for 1/2/4;
index 2.308/2.447/2.420ms; install 4.221/5.176/4.419ms). Do not sum overlapping
concurrent command durations or call those wall timers server CPU.

## Reclamation and acceptance boundary

Normal cleanup is observed for 30 seconds, then an explicit owned sweep drains
remaining garbage. The normal interval is censored: it is not normal GC's full
completion time. JSON records remaining directories/members after that interval,
explicit sweep count/time and final zero queues/directories. Engine SST reclamation
and OS caches are not reset; logical ownership/GC drain does not prove all Flash
bytes have returned to the filesystem. Alternating trials retain this shared-
process allocator/cache/engine carryover limitation.

Published immutable roots are readable before compaction quiet. Backup readiness
is unverified and no backup was attempted; the maintenance tail is not claimed
to delay queries or backups. The observed transient physical directory peak and
Flash usage remain relevant capacity costs even if callers do not wait for quiet.

The isolated controls, sustained concurrent controls and previously recorded
failure/recovery/QPF4 native suites support #565's scoped acceptance. No new
100M gate is imposed on this issue. Full canonical native ingestion/capacity,
backup compatibility and full-scale throughput remain #566 gaps. These bounded
measurements do not establish a full-scale speedup or explain every historical
low-throughput observation.
