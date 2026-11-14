# Ingestion conversion reuse: evidence only

The frozen candidate `553960585f29a5f7b1151742e97d12168f5a9d69` reuses an
execution-local flat Value matrix and bounded row views after synchronous row
insertion. It clears Value/string/decimal/record references on success and every
error before the input reader advances. It retains at most the existing 4096-row
conversion slice, uses no global pool, keeps row-view capacity equal to width and
leaves unsliced legacy records above that size unpooled. Default evaluation,
column conversion, full-slice checks and managed/unmanaged write semantics stay
in their original order.

**Keep the implementation unapplied.** Allocation and memory results are useful,
but elapsed results do not establish the absence of a regression under the
[acceptance gate](../../../docs/sdlc/changes/ingest-buffer-reuse/spec.md) fixed
before timing. There is no post-hoc threshold, source retuning or selective rerun.
Production Go code on this evidence branch is identical to baseline `88774f10`.
The exact candidate implementation and tests remain in [candidate.patch](candidate.patch)
and the original source commit; applying it is a separate decision.

## Complete native comparison

All **64 trials** independently validated complete 105-field physical types,
values, row multiplicity and the exact 1..10000 ordinal set. Unmanaged output
also passed the raw HASH comparison. Every warmup and measured observation is
in [native-results.json](native-results.json); zero failures, conflicts, retries
or discarded samples occurred. Each binary ran eight cases with one warmup and
three measured repetitions. Baseline ran as one block before candidate; case
order reverses between repetitions.

Both variants use the identical frozen
[helper](../ingest_buffer_compare.py), source SHA and existing run.py validation.
The 100K×105 source is loaded in each worker before selecting 10K rows split
across one/four writers. An ordinal column is added to both modes, plus explicit
application __key for unmanaged input. Input arrays/batches are materialized.
One stream contains 1K batches (ten for one writer; 1000/1000/500 per each of four
writers). One batch contains 10000 or 2500 rows per writer. Each worker makes
one adbc_ingest call. The four-writer/one-batch cells do not reuse a conversion
slice and provide a useful control; no mode's semantics are equated to another's.

|Mode|Shape|Writers|Baseline median s|Candidate median s|Elapsed change|Baseline RSS MiB|Candidate RSS MiB|RSS change|
|---|---|---:|---:|---:|---:|---:|---:|---:|
|managed|one_stream|1|1.8140|1.8319|+1.0%|253.77|233.59|-7.9%|
|managed|one_stream|4|2.3803|2.5227|+6.0%|245.89|230.19|-6.4%|
|managed|one_batch|1|1.5904|1.5873|-0.2%|380.39|326.47|-14.2%|
|managed|one_batch|4|2.4195|2.3024|-4.8%|270.23|270.95|+0.3%|
|unmanaged|one_stream|1|1.2040|1.2418|+3.1%|252.09|227.02|-9.9%|
|unmanaged|one_stream|4|1.0631|1.0405|-2.1%|243.62|224.58|-7.8%|
|unmanaged|one_batch|1|1.2476|1.2405|-0.6%|380.44|322.31|-15.3%|
|unmanaged|one_batch|4|1.0698|1.0791|+0.9%|267.77|267.45|-0.1%|

All six cells that reuse storage have candidate RSS ranges wholly below baseline,
with medians 6.4–15.3% lower. Four-writer/one-batch controls are essentially flat.
RSS is the largest individual worker's peak, including Python/Arrow/the driver;
it is not four workers summed or a measurement of Go retained heap.

The managed one-writer stream has nonoverlapping elapsed ranges: baseline
1.8136–1.8159 seconds, candidate 1.8240–1.8670. Its median is 1.0% higher.
Managed four-writer streaming has a 6.0% higher median, with overlapping ranges
2.3482–2.5768 versus 2.4240–2.5384. Unmanaged one-writer streaming is 3.1% higher
with overlapping ranges. Unmanaged four-writer streaming is 2.1% lower, with
nonoverlapping ranges. Other medians range from −4.8% to +0.9%. Every distribution
is retained in [summary.json](summary.json), so useful memory gains do not hide
the elapsed tradeoff. Shared-host order and three samples prevent assigning all
differences causally to this code. No universal speedup or regression is claimed.

Timed work includes spawned workers, imports, source/batch preparation,
connections, insertion and worker teardown. Table/index setup, readiness,
complete validation and ownership-checked cleanup are outside trial clocks.
Small input batches are not a constant-memory application guarantee. Managed
whole-operation publication, serialized admission and unmanaged per-row partial
acknowledgments are unchanged by this candidate.

## Conversion allocations

The separate microbenchmark runs four conversion slices (1024/1024/1024/17 rows)
per operation with 8/105 columns: integers, or integers plus every third column
string. Every 17th value is NULL; integers exceed 2^53 and strings contain Unicode.
It includes initial matrix allocation, coercion and owned string cloning. Reuse
also pays required clearing; fresh control does not do unnecessary clearing.
Input array construction is outside timing, equally for both. Full-field generic
reference checks precede and follow every timed benchmark. This compares fresh
and reused matrices in candidate code, not two native driver binaries, and
excludes defaults, constraints, Redis and publication.

|Columns|Kind|Fresh median ms/op|Reuse median ms/op|Change|Fresh B/op|Reuse B/op|Fresh allocs/op|Reuse allocs/op|
|---:|---|---:|---:|---:|---:|---:|---:|---:|
|8|integer|0.6679|0.5270|-21.1%|3,842,592|1,275,521|104|98|
|8|mixed|0.9674|0.8250|-14.7%|3,980,881|1,413,808|8783|8777|
|105|integer|5.8378|5.6504|-3.2%|49,430,179|16,410,624|1268|1262|
|105|mixed|9.7155|9.8004|+0.9%|51,043,541|18,023,984|102523|102517|

All 24 samples are in [construction-results.json](construction-results.json).
Wide allocated bytes fall 64.7–66.8%, with six fewer allocations per operation.
Wide integer conversion is 3.2% faster by median; mixed is 0.9% slower. Both
fresh and reused paths still clone input strings; this is bounded reuse, not
zero-copy or input borrowing. GOMAXPROCS=4 and 200 ms per benchmark sample, three
repeats. No full driver speedup follows from allocation evidence alone.

## Correctness, ownership and attempts

Focused unit and race runs each passed three top-level tests / 59 cases with
zero failures/skips: conversion parity, Arrow string ownership, reusable view
bounds and clearing string/decimal/nested-record references across long/short/
empty slices. Software passed eight top-level / 14 cases, zero failures/skips.
The new managed/unmanaged test validates every value after input records sized
4096/3/4097/1, including NULL strings, >2^53 integers and an omitted managed
default. Existing controls cover 100001-row managed success/late input failure,
unmanaged append, partial writes, unknown replies without replay, reader failure,
cancellation and changed schema. [checks.json](checks.json) retains every case.
These are selected tests, not a claim that the full repository suite ran.

Source inspection establishes synchronous ownership: insertRows allocates IDs,
encodes row values to command arguments and completes write/recovery paths before
returning. Managed durable-outcome resolution reads acknowledgments without
retaining the caller's matrix. Unmanaged existence checks and bounded pipelines
read all acknowledgments synchronously. Stored write ranges and index facts
contain IDs/facts, not caller Value slices. Encoded command strings own independent
headers/serialized values; clearing a Value never mutates underlying string
bytes or decimal/record objects. An independent agent and the parent reviewed source/lifecycle and the
helper; this is agent review, not human maintainer approval.

One controller error occurred after every Software test passed: system Python
lacked the redis module for post-test cleanup. No cleanup mutation had run.
The controller was corrected to invoke the existing benchmark virtualenv Python;
only cleanup and the final empty/configuration gate were run, with no test
repetition. Native workers/controllers use that virtualenv. The original tool
failure and recovery record remain private and are summarized in checks.json.
All timed attempts succeeded; no failed test or timed sample was omitted.

## Environment and reproduction

Baseline clean `88774f10cf16fc4260e6ad5077097f4de8828406`, version
`v0.1.0-dev.allocation-base`; candidate clean `553960585f29a5f7b1151742e97d12168f5a9d69`,
version `v0.1.0-dev.ingest-reuse-1`. Both use Go 1.27.3 and versioned Makefile
builds with vcs.modified=false. Native GOMAXPROCS/GOFLAGS/GOWORK are removed;
GOGC/GOMEMLIMIT/GODEBUG were unset. Binary/source/helper hashes, package versions,
all distributions and raw private log hashes are in [provenance.json](provenance.json).

Actual post-loan API/INFO verification found Software 8.2.6-25, Redis 8.8.0,
Search 80808; [environment.json](environment.json) records observed settings.
Active/effective 4x QPF, six Search workers, mnp/32 connections, one 4-GiB
noeviction shard, strict certificate/hostname data TLS and RESP3 were verified
before/after live work. Container limits were four CPUs/eight GiB. One explicitly
recorded local image ID was used throughout without tag resolution; this is not
a registry digest claim. Host Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3
arm64. Management bootstrap uses its self-signed trust mode separately from
strict data TLS.

Use an owned Software fixture provisioned with `make -C go redis-up`, verify its
effective configuration, and privately set REDIS_BENCH_* / SSL_CERT_FILE. Build
baseline and candidate through the versioned Makefile commands, applying the
frozen patch in an isolated candidate checkout. Run the same helper separately
for both binaries with --write-repeats 3 and fresh private --work directories.
The helper never provisions infrastructure and requires an owned empty database.
Run the frozen source's BenchmarkIngestBufferConversion with -benchmem,
-benchtime=200ms -count=3 and GOMAXPROCS=4. Do not relabel new runs with these hashes.

The loan ended empty/index-free; every process/client exited and effective
QPF/TLS/RESP3 was reverified. Final parent teardown removed the owned container
and network plus generated TLS/test.env/settings/fixture-env credentials,
preserving the pre-existing container and all 31 pre-existing networks. The
[sanitized cleanup proof](cleanup-proof.json) records the completed outcome.
Generated-credential scans found zero matches requiring redaction, so retained
raw log hashes are unchanged. Raw operational artifacts, endpoint/resource
identifiers, credentials and license files stay private. No ClickBench or
alternative Redis distribution/provider was run.

Audit without Redis or Go:

```sh
python3 benchmarks/table-modes/ingest_buffer_audit.py
```
