# Post-Arrow managed/unmanaged benchmark

Measured November 12, 2026. This is an **unreleased PR #401 snapshot**, source
`426fdb88bbff8561baf5f8f3535edfe14f172985`, built as
`v0.0.26-dev.arrow-bench`; GetInfo verified. These results do not relabel the
historical v0.0.26 release measurements. No 100M ClickBench rerun.

## Existing-table writes

Median seconds for 10,000 rows × 105 public columns plus one ordinal.
Up to 1,000 rows per SQL INSERT, Arrow call or HSET pipeline. Preparation is
outside timing for every path. Three measured trials after one warmup.

| Path | 1 writer | 4 writers |
|---|---:|---:|
| Managed SQL INSERT | 8.954 | 40001 / incomplete |
| Unmanaged SQL INSERT | 9.510 | 5.401 |
| Managed Arrow append | 8.764 | 19.562 |
| Unmanaged Arrow append | 9.083 | 5.245 |
| redis-py+hiredis HSET | 1.217 | 0.886 |

## Reads

Median complete-result seconds for 100,000 rows × 105 columns, 4,096-row Arrow
batch ceiling. Six measured trials after warmup. Four readers process eight
disjoint ordinal-range tasks; one reader processes the complete table.

| Path | 1 reader | 4 readers |
|---|---:|---:|
| Managed SQL | 3.486 | 1.779 |
| Unmanaged SQL | 5.982 | 18.593 |
| redis-py+hiredis Search/HMGET | 4.477 | 2.142 |

The complete matrix also measures 512-row batch ceilings. Values, physical
Arrow types and multiplicities are checked outside timing for every result.
These are explicit SQL ranges, not managed live partition reads or a shared
snapshot across range statements. Mutable source data are held fixed during reads.

## Creation workloads

Median seconds for the same 10K-row write values. The clock includes table/index
creation, worker startup, input/key construction, connections, writes, shutdown
and index readiness. Creating the benchmark namespace and ownership marker is
outside timing. Arrow input is split into calls of up to 1,000 rows: `create`
uses create for the first call then append; `create_append` uses that operation
for every call. Each managed call publishes independently, not the whole 10K load.

| Path / operation | 1 writer | 4 writers |
|---|---:|---:|
| Managed Arrow create | 7.652 | Not run: create has one creator |
| Managed Arrow create_append | 7.815 | 16.883 |
| Unmanaged Arrow create | 7.975 | Not run: create has one creator |
| Unmanaged Arrow create_append | 8.075 | 4.801 |
| Native index creation + HSET | 0.852 | 0.522 |

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

Four concurrent managed SQL conflicts remain failed workloads: no successful
latency/throughput is assigned to incomplete input. Visible partial rows are
independently verified against source ordinals. The earlier v0.0.25/v0.0.26
same-API controls remain in the historical report; SQL and Arrow writer admission
are different contracts. All attempts, including warmups and conflicts, are retained.

Read time includes fresh processes/imports, source-schema loading, connections,
full fetch/conversion, Arrow IPC output and shutdown. First batch is producer-side
before IPC. Write time includes fresh processes, source loading/conversion,
explicit-key and SQL-literal construction where applicable, writes, shutdown and
index readiness. Full validation/preparation/cleanup are outside the timed interval.
Cases alternate forward/reverse order by repetition. Ranges are observations,
not confidence intervals or isolated driver-version speedups.

## Environment and reproducibility

Redis Software 8.2.6-25, Redis 8.8.0, Search 8.8.8; one shard, RAM/noeviction,
4 GiB database, container four CPUs/eight GiB. Effective active 4× QPF, six Search
workers, mnp routing and 32 connections verified before every case. Authenticated
certificate-verified TLS and RESP3. Apple M5 Max arm64, 18 logical CPUs/36 GiB
host RAM; shared workstation with benchmark suites sequential. Go 1.27.3; Python
3.15.2, PyArrow 26.0.1, ADBC manager 1.13.0, redis-py 8.1.0/hiredis 3.5.0.

Source Arrow SHA-256:
`a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2`.
Binary SHA-256: `d2332ce85100035ea5907671e6ac2e49d8be272b94912ef34376517b084e0653`.

[measurements.json](measurements.json) retains sanitized observations;
[summary.json](summary.json) retains recomputed distributions and failure counts.
Run `python3 benchmarks/table-modes/arrow_audit.py` for coverage, status and
arithmetic checks. The independently typed input and operational artifacts remain
private. No full-scale loading benchmark, physical Flex benchmark, Flight SQL rerun,
100M rerun or NativeClient point-read test is claimed.
