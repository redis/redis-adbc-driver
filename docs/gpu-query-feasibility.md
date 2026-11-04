# GPU query feasibility and Arrow device interoperability

Issue: [redis/redis-adbc-driver#215](https://github.com/redis/redis-adbc-driver/issues/215).
Source review: `95d6583`, 2026-11-03. This is the historical investigation and
consumer-prototype assessment at that revision. Subsequent native CUDA predicate
execution and C Device extensions are described in [the implementation guide](gpu-execution.md).
Statements below about absent native support refer to the investigated baseline.

## Interoperability decision

Use explicit **host staging in the consumer** for the first experiment:
Redis HASH/Search → Go CPU values → host Arrow → cuDF device memory → GPU
filter/group/reduce → host Arrow result. This is a consumer-side execution
prototype, not a new SQL planner backend or zero-copy device export.

The pinned ADBC Go module is v1.12.0; arrow-go is v18.8.0. The checked-in
`go/pkg/adbc.h` exposes `ArrowArray`/`ArrowArrayStream` in Bind, BindStream and
ExecuteQuery, with no C Device entry points. `go/pkg/driver.go` imports standard
C Data and exports CPU record batches. `go/arrowconv.go` builds host arrays.
`go/aggregate_scan.go` receives pages of row maps, and `go/parallel_aggregate.go`
merges CPU accumulators. `go/scan_arrow.go`, linked in the original issue, does
not exist at this revision.

arrow-go v18.8.0 contains C Device ABI declarations and async device stream
helpers, but its `arrow/cdata/exports.go:asyncStreamOnSchema` explicitly rejects
non-CPU producers. Header declarations alone do not supply a GPU allocator,
device-buffer importer or GPU execution engine. Upgrading headers or casting
`ArrowDeviceArray` to `ArrowArray` would not provide safe support.

The upstream [C Device interface](https://arrow.apache.org/docs/format/CDeviceDataInterface.html)
remains experimental. Device-resident output is deferred pending a separate
driver-manager/consumer ABI design, allocator and owner lifetime, explicit device
ID, stream/event synchronization, cancellation and release tests with a real
producer/consumer. Ordinary Bind/BindStream require CPU buffers; callers must
stage device data to host first. These APIs lack a device discriminator, so the
driver cannot reliably identify and reject a device pointer disguised as CPU
memory. No such pointer should be passed to them.

The prototype lets cuDF own device allocations and uses the active CUDA device.
It synchronizes before/after upload, computation and download; Python objects
keep the source and results alive until transfer completes. CUDA import/runtime,
allocation and computation errors are fatal, with no silent CPU substitution.
Selecting `--backend cpu` is the explicit portable fallback. This does not
validate C Device import/export or driver device-buffer ownership.

## Query execution hypothesis

Large scans requiring expensive residual filtering/grouping could benefit.
Selective indexed queries, key lookups and small server aggregate results are
poor first candidates: moving all input rows to a GPU can cost more than keeping
the work in Redis. Preserve valid Search projection/filter/aggregate pushdown.
The prototype deliberately scans both columns to isolate consumer execution;
a future planner should push safe predicates before transferring residual work.

The initial supported experiment is:

```sql
SELECT bucket, COUNT(value), MIN(value), MAX(value), SUM(value)
FROM fixture WHERE value >= 0 GROUP BY bucket;
```

Both columns must be nullable int64. NULL values fail the filter; NULL bucket
keys form one group. Python arbitrary-precision integers independently validate
every output group/value and reject any group sum exceeding int64. Included
values are nonnegative, so this also bounds every partial sum. Empty and
all-filtered input produce zero groups. The test includes integers above 2^53.

This restriction is intentional: `go/exactsum.go` preserves exact floating and
decimal reductions, NaNs/infinities and overflow behavior. Ordinary GPU SUM/AVG
does not establish those SQL guarantees. Floats, decimals, strings/collations,
joins, windows, DISTINCT, cancellation during a CUDA kernel, spilling and device
out-of-memory fallback need separate design and validation before driver use.

## Measurement and acceptance

See [the runnable experiment](../benchmarks/gpu-query/README.md). Report CPU
Arrow time, CUDA upload, synchronized compute, result download, and full
host-to-host time. Synthetic results are operator measurements only. Redis mode
fully fetches a shared scan and an equivalent direct SQL aggregate, validates
both, and records direct SQL execute-plus-fetch time. The sum of scan and
backend times is explicitly an estimate, not a separately measured pipeline.
Domain preflight and independent validation are outside the timed regions.

Before promoting any GPU backend:

1. Measure on real CUDA hardware at several row counts, selectivities and group
   cardinalities, including warmups and at least three alternating trials.
2. Use Redis Software with verified active 4x QPF (`make -C go redis-up`) for
   integration comparisons. Keep server version/configuration, client/GPU/driver
   versions, source revision, batch size, RSS/device memory and network topology
   with the results. Keep operational identifiers and credentials private.
3. Compare end-to-end latency against direct SQL with its existing pushdown and
   worker settings. Kernel-only wins do not establish faster SQL execution.
4. Demonstrate result/type equality, empty/NULL cases, overflow, cancellation,
   early close, bounded memory and deterministic CPU fallback. A native backend
   also needs a cost gate that includes conversion, transfer and admission costs.

For now, host-staged consumer execution is feasible to experiment with;
device-resident ADBC output and automatic driver GPU execution remain deferred.
The [EC2 experiment](../benchmarks/gpu-query/README.md) found faster synthetic
grouping at larger sizes, but no query-cost improvement on the one-million-row
Redis workload: direct SQL took 7.40 seconds, versus a modeled 9.71 seconds for
scan plus GPU execution. Input fetch dominated. Hardware measurements and
limitations are in the [validation record](sdlc/changes/215-gpu/validation.md).

## CPU SIMD follow-up

SIMD is worth investigating within the driver because it avoids host/device
transfer. Start with profiles of decoding, allocation and residual evaluation;
the scan measurement above does not isolate Redis wait from client CPU work.
The existing [integer input evaluator](../go/aggregate_inputs.go) is a lazy
per-row DAG over maps, not a contiguous column batch. Introduce typed batches
only if their construction cost is justified by the measured workload.

Candidate kernels are integer predicate masks, NULL bitmap operations and
checked integer expression evaluation feeding the existing exact accumulators.
SIMD integer wraparound must not replace overflow errors; batching must preserve
the original row/aggregate error order and NULL evaluation rules. Floating and
decimal SUM/AVG still require the driver's exact semantics. Parsing can be
another candidate if profiles show byte processing rather than allocation or
network wait dominates. No SIMD speedup has been measured in this change.

The GPU experiment baseline used Go 1.26.6.
[Go 1.26](https://go.dev/doc/go1.26) has experimental amd64
`simd/archsimd` support behind `GOEXPERIMENT=simd`.
[Go 1.27](https://go.dev/doc/go1.27) adds ARM64 NEON and an experimental portable
`simd` API. A production implementation needs architecture dispatch and a
scalar fallback. The subsequent [Go 1.27.3 upgrade](sdlc/changes/go-1.27.3/intent.md)
updates the toolchain, but does not enable experimental SIMD in release builds.
