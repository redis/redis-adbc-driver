# Merged-main performance comparison

Measured November 13, 2026, after PRs #408 and #410–413. Source
`ba855f7995b2685182ed1331bcf6703d73089c4a`, built as `v0.1.0-dev.merged-perf`.
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
| Managed SQL | 2.242 | 1.489 |
| Unmanaged SQL | 5.962 | 2.052 |
| redis-py+hiredis | 4.446 | 2.057 |

Median highest worker RSS per trial, MiB, for the same cases. This includes
Python, imported libraries, source-schema loading, driver/read buffers and IPC
work. It is neither isolated Go heap usage nor total concurrent process memory.

| Path | 1 reader | 4 readers |
|---|---:|---:|
| Managed SQL | 254.6 | 257.7 |
| Unmanaged SQL | 449.1 | 497.1 |
| redis-py+hiredis | 237.8 | 255.0 |

The complete JSON matrix also retains 512-row batch ceilings, first-batch
latencies, every repetition and recomputed ranges. Values, physical Arrow types,
row counts and multiplicities were independently checked outside timing.

## Existing-table writes

Median seconds for 10,000 rows × 105 public columns plus one ordinal; up to
1,000 rows per SQL INSERT, Arrow call or HSET pipeline. Three measured trials
after warmup. Preparation is excluded for every path. One writer makes ten
calls; four writers handle 2,500 rows each and make twelve calls in total.

| Path | 1 writer | 4 writers |
|---|---:|---:|
| Managed SQL INSERT | 7.729 | 16.731 |
| Unmanaged SQL INSERT | 2.498 | 1.413 |
| Managed Arrow append | 7.677 | 17.251 |
| Unmanaged Arrow append | 1.992 | 1.261 |
| redis-py+hiredis HSET | 1.216 | 0.885 |

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
| Managed Arrow create | 7.631 | Not run: one creator |
| Managed Arrow create_append | 7.648 | 16.485 |
| Unmanaged Arrow create | 1.320 | Not run: one creator |
| Unmanaged Arrow create_append | 1.320 | 0.853 |
| Native index + HSET | 0.867 | 0.499 |

Creation uses membership-only application indexes for unmanaged/native paths,
versus private indexed snapshot storage for managed tables. These are workload
comparisons with different consistency/index work. Existing-table unmanaged
SQL/Arrow and HSET use the same full-field application index.

## Historical comparison

Earlier full main source `f630e117`, built as `v0.0.26-dev.main-perf`, versus
this merged build. Same typed input, workload and documented fixture limits;
different revisions and times on a shared workstation. Multiple changes are
included, so differences do not isolate one PR or establish confidence intervals.

| Read path, 4096 ceiling | Readers | Earlier seconds | Merged seconds |
|---|---:|---:|---:|
| Managed SQL | 1 | 2.234 | 2.242 |
| Managed SQL | 4 | 2.111 | 1.489 |
| Unmanaged SQL | 1 | 6.002 | 5.962 |
| Unmanaged SQL | 4 | 2.024 | 2.052 |
| redis-py+hiredis | 1 | 4.329 | 4.446 |
| redis-py+hiredis | 4 | 2.112 | 2.057 |

Historical median highest-worker RSS for the same 4096-row read cases, MiB:

| Read path | Readers | Earlier MiB | Merged MiB |
|---|---:|---:|---:|
| Managed SQL | 1 | 273.0 | 254.6 |
| Managed SQL | 4 | 213.8 | 257.7 |
| Unmanaged SQL | 1 | 456.6 | 449.1 |
| Unmanaged SQL | 4 | 492.0 | 497.1 |
| redis-py+hiredis | 1 | 235.1 | 237.8 |
| redis-py+hiredis | 4 | 278.2 | 255.0 |

Managed four-reader elapsed time was observed 29.5% lower, but highest-worker
RSS 20.6% higher than that earlier full run. Managed one-reader RSS was 6.7%
lower, with essentially unchanged elapsed time. Retain this tradeoff when
comparing the complete revisions; the later matched memory-only study used a
different baseline and should not relabel this historical full-matrix result.
The [earlier full report](../main-perf-2026-11-12/report.md) and its raw
observations remain unchanged.

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
Native library SHA-256: `dab0555209c3f61506aa1f5e23d33b3ff4a6351eb35dd67341af803c7507a190`.
CLI SHA-256: `5913a0c906d1cd88f20537dca4dac027db511a8194d5671b52a1708004fe870f`.

[measurements.json](measurements.json) retains sanitized observations and
[summary.json](summary.json) retains recomputed distributions, RSS medians and
failure counts. Audit with `python3 benchmarks/table-modes/merged_perf_audit.py`.
Run the unchanged `arrow_compare.py --suite all` on an owned empty fixture
verified through `make -C go redis-up`; see the [harness instructions](../README.md).
Raw logs, source data, endpoints, credentials, infrastructure identifiers and
keys remain private. Cleanup evidence is recorded in the linked
[validation record](../../../docs/sdlc/changes/merged-performance-rerun/validation.md).
