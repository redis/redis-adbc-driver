# Main performance benchmark

Measured November 12, 2026. This is **unreleased main after PRs #403–405**, source
`f630e117201752b6ccf8c2dcb4da1d3e17203c3a`, built as
`v0.0.26-dev.main-perf`; GetInfo verified. These results do not relabel the
historical v0.0.26 release measurements. No 100M ClickBench rerun.

## Existing-table writes

Median seconds for 10,000 rows × 105 public columns plus one ordinal.
Up to 1,000 rows per SQL INSERT, Arrow call or HSET pipeline. Preparation is
outside timing for every path. Three measured trials after one warmup.
One writer makes ten calls/pipelines; four writers each handle 2,500 rows,
so they make twelve calls/pipelines in total.

| Path | 1 writer | 4 writers |
|---|---:|---:|
| Managed SQL INSERT | 7.886 | 17.783 |
| Unmanaged SQL INSERT | 2.497 | 1.397 |
| Managed Arrow append | 7.840 | 17.025 |
| Unmanaged Arrow append | 2.020 | 1.281 |
| redis-py+hiredis HSET | 1.222 | 0.883 |

## Reads

Median complete-result seconds for 100,000 rows × 105 columns, 4,096-row Arrow
batch ceiling. Six measured trials after warmup. Four readers process eight
disjoint ordinal-range tasks; one reader processes the complete table.

| Path | 1 reader | 4 readers |
|---|---:|---:|
| Managed SQL | 2.234 | 2.111 |
| Unmanaged SQL | 6.002 | 2.024 |
| redis-py+hiredis Search/HMGET | 4.329 | 2.112 |

The complete matrix also measures 512-row batch ceilings. Values, physical
Arrow types and multiplicities are checked outside timing for every result.
These are explicit SQL ranges, not managed live partition reads or a shared
snapshot across range statements. Mutable source data are held fixed during reads.

## Historical read context

Earlier source `426fdb88` versus latest `f630e117`, median seconds at the
4,096-row batch ceiling. Same typed input and documented server settings;
different revisions and points in time on a shared workstation. This comparison
does not isolate one PR or establish a confidence interval. The four-reader
managed case is slower here despite the faster one-reader case.

| Path | Readers | Earlier | Latest |
|---|---:|---:|---:|
| Managed SQL | 1 | 3.486 | 2.234 |
| Managed SQL | 4 | 1.779 | 2.111 |
| Unmanaged SQL | 1 | 5.982 | 6.002 |
| Unmanaged SQL | 4 | 18.593 | 2.024 |
| redis-py+hiredis | 1 | 4.477 | 4.329 |
| redis-py+hiredis | 4 | 2.142 | 2.112 |

## Creation workloads

Median seconds for the same 10K-row write values. The clock includes table/index
creation, worker startup, input/key construction, connections, writes, shutdown
and index readiness. Creating the benchmark namespace and ownership marker is
outside timing. Arrow input is split into calls of up to 1,000 rows: `create`
uses create for the first call then append; `create_append` uses that operation
for every call. Each managed call publishes independently, not the whole 10K load.

| Path / operation | 1 writer | 4 writers |
|---|---:|---:|
| Managed Arrow create | 7.670 | Not run: create has one creator |
| Managed Arrow create_append | 7.500 | 17.504 |
| Unmanaged Arrow create | 1.295 | Not run: create has one creator |
| Unmanaged Arrow create_append | 1.313 | 0.837 |
| Native index creation + HSET | 0.873 | 0.526 |

Unmanaged/native creation uses a membership-only Search index (TAG NOINDEX,
SKIPINITIALSCAN). Managed creation maintains private indexed snapshot storage.
The existing-table suite uses the same full-field application index for unmanaged
SQL, unmanaged Arrow and native HSET. Creation is a workload comparison with
different index/consistency work; it does not isolate a single setup cost.

## Interpretation and timing

Unmanaged Arrow writes are direct inserts into application-owned HASHes, with
explicit new __key values and no data-publication step or rollback. Raw Redis
clients can access those HASHes directly. The raw client here is redis-py with
hiredis, not the managed snapshot-aware NativeClient wrapper. Managed SQL/Arrow
writes maintain immutable generations and atomic publication per statement/call.
No SQL, Arrow or uncertain mutation is automatically replayed.

All 156 trials, including warmups, completed and independently validated. No
conflicts or automatic retries occurred. Managed SQL DML and Arrow append share
same-table leased admission before execution/input consumption. Four workers
submit concurrently but admitted calls serialize; no parallel write speedup is
implied. DDL, additional CTE targets and changed managed read dependencies can
still conflict. The earlier failed SQL workloads remain historical evidence.

Read time includes fresh processes/imports, source-schema loading, connections,
full fetch/conversion, Arrow IPC output and shutdown. First batch is producer-side
before IPC. Write time includes fresh processes, source loading/conversion,
explicit-key and SQL-literal construction where applicable, writes, shutdown and
index readiness. Full validation and cleanup are outside the timed interval.
Existing-table setup is excluded; creation includes table/index setup as above.
Cases alternate forward/reverse order by repetition. Ranges are observations,
not confidence intervals or isolated driver-version speedups. The historical
[post-Arrow report](../post-arrow-2026-11-12/report.md) used the same data/workload
and server settings, but a different source and point in time. This run contains
both managed and unmanaged optimizations plus SQL admission; differences cannot
be attributed to one PR in isolation.

## Environment and reproducibility

Redis Software 8.2.6-25, Redis 8.8.0, Search 8.8.8; one shard, RAM/noeviction,
4 GiB database, container four CPUs/eight GiB. Effective active 4× QPF, six Search
workers, mnp routing and 32 connections verified before every case. Authenticated
certificate-verified TLS and RESP3. Apple M5 Max arm64, 18 logical CPUs/36 GiB
host RAM; shared workstation with benchmark suites sequential. Go 1.27.3; Python
3.15.2, PyArrow 26.0.1, ADBC manager 1.13.0, redis-py 8.1.0/hiredis 3.5.0.

Source Arrow SHA-256:
`a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2`.
Binary SHA-256: `6c427fb2bb07b8686ecdc5a81bab9677970b9f68068ec8d538ea23daf2597e2e`.

[measurements.json](measurements.json) retains sanitized observations;
[summary.json](summary.json) retains recomputed distributions and failure counts.
Run `python3 benchmarks/table-modes/main_perf_audit.py` for coverage, status and
arithmetic checks. The independently typed input and operational artifacts remain
private. No full-scale loading benchmark, physical Flex benchmark, Flight SQL rerun,
100M rerun or NativeClient point-read test is claimed.
