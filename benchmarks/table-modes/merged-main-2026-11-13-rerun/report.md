# Merged-main performance rerun

Measured November 13, 2026, after PRs #415–418. Source
`5f759300c12f5191a36d2f2b5a29555ef14ef486`, built as `v0.1.0-dev.merged-perf-2`.
This is an unreleased merged-main build; historical release results retain their
original labels. All **156 trials completed and fully validated**, including
warmups. No conflicts, failed trials or caller retries. No 100M ClickBench rerun.

## Reads

Median complete-result seconds for 100,000 rows × 105 public columns,
4,096-row Arrow batch ceiling; six measured repetitions after warmup. Four
readers process eight disjoint ordinal ranges. These are separate SQL range
statements over fixed data, not shared live-partition snapshots.

| Path | 1 reader | 4 readers |
|---|---:|---:|
| Managed SQL | 2.246 | 1.458 |
| Unmanaged SQL | 4.903 | 2.019 |
| redis-py+hiredis | 4.260 | 2.075 |

Median highest worker RSS per trial, MiB, for the same cases. This includes
Python, imported libraries, source-schema loading, driver/read buffers and IPC
work. It is neither isolated Go heap usage nor total concurrent process memory.

| Path | 1 reader | 4 readers |
|---|---:|---:|
| Managed SQL | 255.8 | 255.7 |
| Unmanaged SQL | 263.7 | 493.1 |
| redis-py+hiredis | 235.2 | 251.4 |

The complete JSON matrix also retains 512-row batch ceilings, first-batch
latencies, every repetition and recomputed ranges. Values, physical Arrow types,
row counts and multiplicities were independently checked outside timing.

## Existing-table writes

Median seconds for 10,000 rows × 105 public columns plus one ordinal; up to
1,000 rows per SQL INSERT, Arrow call or HSET pipeline. Three measured trials
after warmup. Table/index setup is excluded for every path. One writer makes ten
calls; four writers handle 2,500 rows each and make twelve calls in total.

| Path | 1 writer | 4 writers |
|---|---:|---:|
| Managed SQL INSERT | 7.830 | 17.277 |
| Unmanaged SQL INSERT | 2.519 | 1.394 |
| Managed Arrow append | 7.791 | 17.428 |
| Unmanaged Arrow append | 2.002 | 1.234 |
| redis-py+hiredis HSET | 1.228 | 0.924 |

Managed SQL and Arrow calls share same-table leased admission. Four callers
submit concurrently but admitted writes serialize; no parallel managed-write
speedup is implied. Each statement/Arrow call publishes independently. Unmanaged
Arrow appends directly to application-owned HASHes with explicit new keys,
without managed data publication or rollback. The raw client is redis-py with
hiredis, not the managed snapshot-aware NativeClient wrapper.

## Creation workloads

Median seconds for the same 10K-row values, including table/index creation,
worker startup, input/key construction, connections, writes, shutdown and index
readiness. Arrow inputs use calls of up to 1,000 rows. `create` has one creator
and appends after its first call; `create_append` is used for every call.

| Path / operation | 1 writer | 4 writers |
|---|---:|---:|
| Managed Arrow create | 7.675 | Not run: one creator |
| Managed Arrow create_append | 7.643 | 16.444 |
| Unmanaged Arrow create | 1.354 | Not run: one creator |
| Unmanaged Arrow create_append | 1.312 | 0.850 |
| Native index + HSET | 0.867 | 0.502 |

Creation uses membership-only application indexes for unmanaged/native paths,
versus private indexed snapshot storage for managed tables. These are workload
comparisons with different consistency/index work. Existing-table unmanaged
SQL/Arrow and HSET use the same full-field application index.

## Historical comparison

Previous full source `ba855f79`, built as `v0.1.0-dev.merged-perf`, versus
this merged build. Same typed input, workload and documented fixture limits;
different revisions and times on a shared workstation. Multiple changes are
included, so differences do not isolate one PR or establish confidence intervals.

| Read path, 4096 ceiling | Readers | Earlier seconds | Merged seconds |
|---|---:|---:|---:|
| Managed SQL | 1 | 2.242 | 2.246 |
| Managed SQL | 4 | 1.489 | 1.458 |
| Unmanaged SQL | 1 | 5.962 | 4.903 |
| Unmanaged SQL | 4 | 2.052 | 2.019 |
| redis-py+hiredis | 1 | 4.446 | 4.260 |
| redis-py+hiredis | 4 | 2.057 | 2.075 |

Historical median highest-worker RSS for the same 4096-row cases, MiB:

| Read path | Readers | Previous MiB | Rerun MiB |
|---|---:|---:|---:|
| Managed SQL | 1 | 254.6 | 255.8 |
| Managed SQL | 4 | 257.7 | 255.7 |
| Unmanaged SQL | 1 | 449.1 | 263.7 |
| Unmanaged SQL | 4 | 497.1 | 493.1 |
| redis-py+hiredis | 1 | 237.8 | 235.2 |
| redis-py+hiredis | 4 | 255.0 | 251.4 |

The one-reader unmanaged median took 17.8% less elapsed time
and 41.3% less highest-worker RSS than the previous full run.
This is an observation across complete revisions and time on a shared workstation,
not an isolated attribution or confidence interval. The
[earlier full report](../merged-main-2026-11-13/report.md) and every historical
sample remain unchanged. The merged production improvement is eligible unmanaged
direct Arrow projection ([#416](https://github.com/redis/redis-adbc-driver/pull/416)).
The reply-budget and early-index prototypes from
[#417](https://github.com/redis/redis-adbc-driver/pull/417) and
[#418](https://github.com/redis/redis-adbc-driver/pull/418) remain unapplied. Four-reader filtered
unmanaged queries retain the residual/HGETALL path; no direct-projection
improvement for that path is claimed.

## Environment, timing and reproduction

Redis Software 8.2.6, Redis 8.8.0, Search 8.8.8; one RAM shard,
4 GiB/noeviction, container limited to four CPUs/eight GiB. Active/effective
4x QPF, six Search workers, mnp routing and 32 connections were verified before
every case and after the final case. Data connections use authenticated,
certificate-verified TLS and RESP3. The local management API retains its
fixture bootstrap self-signed certificate.

Apple M5 Max, 18 logical CPUs/36 GiB; Go 1.27.3, Python 3.15.2, PyArrow 26.0.1,
ADBC manager 1.13.0, redis-py 8.1.0/hiredis 3.5.0. The suite ran sequentially
on a shared workstation, with task builds/setup completed before timing.

Read time includes fresh processes/imports, source-schema loading, connections,
full fetch/conversion, Arrow IPC output and shutdown. First-batch latency is
producer-side before IPC. Write time includes fresh processes, source loading
and conversion, explicit keys/SQL literals, writes, shutdown and index readiness.
Full validation and cleanup are outside the timed interval. Cases alternate
forward/reverse order by repetition. All attempts and warmups are retained.

Source Arrow SHA-256: `a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2`.
Native library SHA-256: `7048adb0e0b992c0636d02d4aef12128c1f2fcaf58bc075c4a783ef04d1b17a8`.
CLI SHA-256: `2dca79ce55abe3020aa55aaffa47e1a058c48bc6d2231f6a09d798091e049d2a`.

[measurements.json](measurements.json) retains sanitized observations and
[summary.json](summary.json) retains recomputed distributions, RSS medians and
failure counts. Audit with `python3 benchmarks/table-modes/merged_perf_rerun_audit.py`.
Run the unchanged `arrow_compare.py --suite all` on an owned empty fixture
verified through `make -C go redis-up`; see the [harness instructions](../README.md).
Raw logs, source data, endpoints, credentials, infrastructure identifiers and
keys remain private. Cleanup evidence is recorded in the linked
[validation record](../../../docs/sdlc/changes/merged-performance-rerun-2/validation.md).
