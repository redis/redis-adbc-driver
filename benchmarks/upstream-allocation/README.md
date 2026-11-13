# Isolated upstream allocation fixtures

These fixtures support [go-redis #4299](https://github.com/redis/go-redis/issues/4299)
and [Arrow Go #1548](https://github.com/apache/arrow-go/issues/1548). They attribute
specific library work; they do not assume either library is an end-to-end driver
bottleneck. They do not run a Redis server or benchmark the ADBC driver.

The [November 13 evidence report](2026-11-13/report.md) preserves all 84 audited
samples and the ownership/measurement limitations.

Pinned dependencies are go-redis **v9.23.0**, Arrow Go **v18.8.0** and Go **1.27.3**.
The runner records module checksums, fixture/implementation SHA-256 values, CPU,
OS/architecture and Go versions. It never edits the shared module cache. A fresh
private copy of go-redis retains its original BSD license; only copied root test
files are replaced by the isolated parser fixture. No upstream test-suite result
is claimed. New harness files use this repository's Apache 2.0 license.

## Reproduction

From the repository root, on a host with Go and a C compiler:

```sh
python3 benchmarks/upstream-allocation/run.py --mode check --work /private/new-check
python3 benchmarks/upstream-allocation/run.py --mode bench --work /private/new-run \
  --benchtime 200ms --count 3 --gomaxprocs 1
python3 benchmarks/upstream-allocation/summarize.py /private/new-run \
  --output /private/new-run/summary.json
```

Use new directories; previous evidence is never overwritten. Coordinate an
exclusive host measurement slot. The runner first performs untimed parser and
race-enabled Arrow lifetime checks, then runs benchmarks separately. It removes
Redis environment variables and uses only in-memory bytes and C callbacks.
All raw attempts/logs stay private; publish only audited, sanitized evidence.

## RESP3 parser boundary

The injected fixture runs **actual `SliceCmd.readReply` / `proto.Reader` code**
from the pinned module. It decodes 105-field HMGET-shaped RESP3 arrays in pages of
256/1024/4096 rows, with reader buffers of 32 KiB and 1 MiB. Fixtures distinguish
missing fields from empty strings and include exact integer bytes above 2^53,
Unicode, CRLF and embedded NUL bytes. Every field/reply is consumed. Complete
byte/type/null equality is checked outside timing before and after each case;
the timed consumer counts presence/length and a leading byte for each value.

Cases compare:

- `slice_cmd`: the real generic owned-result decoder; command construction,
  network, TLS, hooks, pool, scheduling and Arrow conversion are excluded.
- `string_slice_reuse`: a benchmark-only typed consumer using the existing
  internal `ReadString` primitive and reused result/null slices. String payload
  ownership remains separate from the reused slots.
- `byte_slots_prototype`: a lower-bound prototype using existing internal
  `ReadStringInto` with fixed 64-byte value slots plus CRLF space and explicit
  lengths/null masks. Oversized values fail; a test checks protocol alignment.

`GetToBuffer` already exposes `ReadStringInto` for GET in this version; this
fixture does not request redundant GET support. The array prototypes are **not
public HMGET APIs**, and do not preserve arbitrary retained `SliceCmd` values or
result-inspecting hooks. They cannot justify a production borrowed-buffer change
without an explicit lifetime/hook contract. The byte-slot prototype also reserves
substantial memory up front: the 4096-row value slab alone is 27.1 MiB, excluding
lengths, null masks and slice headers. Low B/op does not mean low resident memory.

Reported MB/s is in-memory wire-byte parsing/consumption throughput. Pipeline
round trips, server execution and Redis compatibility are not measured.

## Arrow C boundary

Each operation hands off the same 10K rows across 105 columns (repeated Int16,
Int32, exact Int64, UTF-8, Date32, UTC microsecond timestamps and Boolean fields).
It compares ten 1K batches with one 10K batch. Fixture construction/application
conversion is outside timing. Every output value/type/null is validated before
and after each timed case; timed iterations consume all batch row counts.

- `go_retain` is a reference-count bookkeeping control, without any C boundary.
- `c_data_schema_each_fresh` exports/imports schema for each batch and allocates
  fresh caller descriptor headers.
- `c_data_schema_each_reuse` preserves per-batch schema semantics but reuses only
  the caller's empty, moved-from descriptor headers.
- `c_data_schema_known_reuse` uses the supported schema-known import API. It is a
  different consumer contract, not a transparent optimization of schema-each.
- `c_stream_schema_once` creates and consumes a complete C Stream; the schema is
  negotiated once per stream, with export/import callbacks for each batch.

C descriptors are zeroed C allocations, not Go pointer-containing memory. Header
reuse occurs only after ownership moves into an imported record. Tests retain
records across descriptor reuse, reader advance, full/early reader release and
concurrent independent streams. All original input references are released, and
checked allocators must return to zero without depending on garbage collection.
Early release models consumer abandonment; no context cancellation API is claimed.

**Go B/op and allocs/op exclude C malloc/calloc.** Wall time includes C calls and
C allocation/release work; this is not a C-allocation count or peak-RSS measure.
Record payload buffers are shared, not serialized/copied by these handoffs. IPC,
Python, transport and Redis costs are excluded. C Stream/schema-known comparisons
must retain their different metadata/lifetime semantics in any conclusion.

### Producer reference accounting

Arrow v18.8.0's `ExportRecordReader` comment says caller Release is unnecessary,
but its implementation independently retains the producer. The fixture follows
the implementation and explicitly tests the full chain:

1. `array.NewRecordReader` creates the caller's reference (count 1).
2. `cdata/exports.go:184–191` retains the exporter's reference (count 2).
3. The caller releases its original reference (count 1).
4. Import moves the C Stream into the imported reader; releasing that reader
   invokes `streamRelease`, then `cRecordReader.release` (count 0).
5. Independently retained output arrays keep their own buffers alive until their
   final Release, even after producer/consumer teardown.

Without step 3, the initial checked-allocator fixture retained all source
buffers. The counted-reader reproducer and allocator checks pass with it. This
is a pinned upstream **comment/implementation mismatch**, not evidence of a
driver leak: this driver's custom `go/pkg/driver.go` exporter has a separate,
balanced caller/stream reference path.

## Companion transport harness

[ndyakov/go-redis-road-test](https://github.com/ndyakov/go-redis-road-test/tree/f8701d65c863ca96ce0d2c4c458e4aa4a945b60c)
already includes a 105-field wide HMGET workload referencing #4299, pinned to
go-redis v9.23.0, with nil/empty/string/>2^53 validation. Its pool, batching,
full-duplex, controlled RTT, p50/p99, allocation/CPU/RSS and server-count controls
are useful transport companions to this parser fixture. Its async window cap
of 256 is not the same shape as explicit 256/1024/4096-row pipeline pages.

It has not been run, adopted or copied here. At the reviewed commit, the engine,
statistics and custom dialer paths use plain TCP without TLS configuration;
GitHub's API did not detect a license. Before adapting/running it:

1. Verify licensing or obtain permission; preserve its provenance.
2. Add certificate-verified TLS and RESP3 to **every** engine, seed, probe,
   statistics and admin connection, including custom dialers.
3. Use an owned Redis Software fixture created by `make -C go redis-up`, verify
   active/effective 4x QPF and its limits, and keep operational artifacts private.
4. Match schemas, row/pipeline sizes, connection counts and consumption lifetime;
   keep explicit-pipeline and async/full-duplex experiments separately labeled.
5. Validate all results, report errors/retries and compare transport profiles
   against these parser-only controls. Never extrapolate to complete managed
   publication, ADBC C Stream or Python conversion without measuring them.
