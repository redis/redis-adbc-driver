# v0.0.26 managed, unmanaged and redis-py comparison

Measured November 12, 2026 using exact release v0.0.26,
`5c9d583a567e1f6891e6d6ef8703da6189957a04`; ADBC GetInfo verified.
All 84 read trials and 20 complete write trials passed independent typed value
and multiplicity checks. Four managed concurrent-SQL trials (including warmup)
returned SQLSTATE 40001; their visible partial rows also passed value checks.
Those failed trials have no successful-load latency or throughput claim.

Median seconds, warmups excluded:

| Path | Read 100K, 1 reader | Read 100K, 4 readers | Write 10K, 1 writer | Write 10K, 4 writers |
|---|---:|---:|---:|---:|
| Managed SQL | 3.432 | 1.786 | 7.798 | 40001 conflicts |
| Unmanaged SQL | 5.977 | 18.755 | 9.517 | 5.280 |
| redis-py+hiredis | 4.487 | 2.168 | 1.205 | 0.882 |

Reads show 4096-row Arrow batch ceilings; the full matrix also measures 512.
Four readers process eight disjoint ordinal ranges. This measures explicit SQL
ranges, **not** managed live partitions. The unmanaged four-reader range workload
is slower here; these measurements do not isolate a planner or server bottleneck.
No general claim that unmanaged tables scale worse follows from this fixture.

## Equivalent values, different guarantees

The independently typed input contains 100,000 rows and 105 public columns:
int16/int32/int64, UTF-8, date32 and microsecond timestamps, without NULLs.
Source SHA-256 is
`a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2`.
An additional ordinal is stored for identical range selection and omitted from
all read projections. Managed SQL reads a separately created private snapshot;
unmanaged SQL and redis-py read exactly the same application-owned HASHes and
Search index. The fixtures are immutable during reads.

Managed SQL has statement snapshot/publication semantics. Unmanaged SQL reads
live application HASHes and writes those source HASHes; it has no managed
snapshot and multirow writes can be partial. Ordinary redis-py commands access
those same HASHes directly. This benchmark does not use the managed snapshot
point-reader [NativeClient](https://github.com/redis/redis-adbc-driver/blob/v0.0.26/docs/native-client.md).
Never write raw HSETs into published managed HASHes.

Reads include fresh worker processes/imports, connection setup, fetch, typed
conversion, Arrow IPC output and shutdown. First-batch timestamps are producer
RecordBatch availability before IPC, not consumer display. One reader issues
one whole-table query; four readers issue eight disjoint range queries. SQL
selects all 105 public columns; redis-py uses Search cursor discovery and HMGET
then converts values into the exact same Arrow types. Client batching ceilings
are matched; server query/scan work is not asserted identical. One warmup and
six measured trials per mode/reader-count/batch-size; order alternates.
Every complete result is checked outside timing for all types, values and
multiplicities, with ordering normalized.

Writes use 10,000 rows x 105 public columns plus ordinal, one/four fresh workers,
and up to 1000 rows per SQL INSERT or native HSET pipeline. Unmanaged INSERT also
supplies explicit source keys. Empty-table/index setup, registration and seed
removal are outside timing; process startup, source conversion, SQL construction,
connections, writes, shutdown and index readiness are inside. Validation is
outside. Native pipelined HSET is neither SQL nor managed atomic publication.
One warmup and three measured repetitions per mode/writer count. No retries.
Unmanaged Arrow ingestion was explicitly attempted and refused with
NotImplemented, as documented; SQL INSERT is used for both SQL modes.
This write method and 10K size differ from the historical v0.0.25 100K Arrow
multi-append experiment, so their load times are not version speedups.

## Why the managed concurrent SQL conflicts are not a new regression

A separate correctness control runs 100 rows with four barrier-synchronized
writers, 25 rows each, three trials per API and release. Each release runs in
its own process, avoiding multiple Go shared runtimes in one process.

| Release | SQL INSERT, each of 3 trials | Arrow append, each of 3 trials |
|---|---|---|
| v0.0.25 | 1 success, 3 conflicts; 25 rows visible | 4 successes; 100 rows visible |
| v0.0.26 | 1 success, 3 conflicts; 25 rows visible | 4 successes; 100 rows visible |

All visible values were checked and matched acknowledged writes. No automatic
retries were used. Concurrent same-table SQL statements can pin the same catalog
state; after one publication the others reject a changed dependency with 40001.
Participating Arrow append calls use a table admission gate before consuming
input. The earlier four-writer Arrow success therefore does not establish
conflict-free four-writer SQL. See the release
[snapshot contract](https://github.com/redis/redis-adbc-driver/blob/v0.0.26/docs/snapshots.md)
and [admission implementation](https://github.com/redis/redis-adbc-driver/blob/v0.0.26/go/snapshot_admission.go).
These small controls establish observed API behavior, not production throughput.

## Environment and limits

Dedicated Redis Software 8.2.6-25, Redis 8.8.0 and Search 8.8.8; one shard,
RAM/noeviction, 4 GiB database; server container four CPUs/eight GiB. Effective
4x QPF, six Search workers, mnp routing and 32 connections checked before each
case. Authenticated certificate-verified TLS, RESP3. Apple M5 Max macOS arm64,
18 logical CPUs and 36 GiB host RAM; shared workstation, suites run sequentially.
Go 1.27.3; Python 3.15.2; PyArrow 26.0.1; ADBC manager 1.13.0;
redis-py 8.1.0 and hiredis 3.5.0. These local timings are not confidence intervals
or isolated version comparisons. Exact binary/source hashes and all observations
are in [measurements.json](measurements.json); recomputed distributions are in
[summary.json](summary.json); API controls are in
[concurrency-controls.json](concurrency-controls.json).

No 100M ClickBench rerun, Flex performance test, Flight SQL rerun or NativeClient
point-read comparison was performed. Other deck suites retain their v0.0.25
labels, and full-scale slides retain historical server/build labels. v0.0.26
supports the Flex stopgap for managed and unmanaged tables; this RAM experiment
does not measure its performance. Operational identifiers, endpoints, credentials,
licenses, raw logs, datasets and slide-edit payloads remain private.

Run `python3 benchmarks/table-modes/audit.py` to check exact trial coverage,
validation/conflict statuses, control outcomes and all summary arithmetic.
