# Exact integer SIMD experiment (#296)

This isolated prototype does not enable a SQL backend. It measures signed int64
comparison masks, NULL bitmap intersection, and conversion of decoded maps.
The existing lazy DAG and exact CPU reductions remain in use.
See the [change specification](../../docs/sdlc/changes/296-simd/spec.md).

From `go/`, with the pinned Go 1.27.3 toolchain and a C compiler:

```sh
go test -count=1 -v ./internal/intpredicate
CGO_ENABLED=0 go test -count=1 ./internal/intpredicate
go test -tags purego -count=1 ./internal/intpredicate
go test -race -count=1 ./internal/intpredicate
go test -run '^$' -bench BenchmarkMatch -benchmem -count=3 ./internal/intpredicate
go test -run '^$' -bench BenchmarkIntegerBatchConversion -benchmem -count=3 .
```

The retired `Integer SIMD experiment` workflow recorded these measurements on
native hosted amd64 and arm64 hardware. It was removed after the completed
experiment retained its no-go decision for automatic SQL integration. The
commands above remain available for manual reproduction. Emulation and
cross-compilation are not hardware validation.
Runtime dispatch uses SSE4.2 or NEON, with a portable scalar fallback. Results
include output allocation and cgo overhead; full conversion includes input/NULL
bitmap allocation and decoded-map lookup. Compare `row` to `convert-dispatch`,
not just the resident-input kernel ratios. Tiny timings need repeated trials.

Go's `simd`/`simd/archsimd` APIs are experimental in the pinned toolchain and
require `GOEXPERIMENT=simd`. This experiment instead uses stable C intrinsics
behind runtime dispatch; it changes no release flags. Any later adoption of
Go experimental SIMD requires a separate compatibility decision. Check the
installed Go source `src/simd/doc.go` and `src/internal/goexperiment` when
changing toolchains rather than assuming experimental API stability.

For private driver/network profiles, from the repository root:

```sh
make -C go redis-up
source go/build/test.env
cd go
REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed REDIS_INTEGER_PROFILE=1 \
  REDIS_INTEGER_PROFILE_ROWS=100000 \
  go test -count=1 -run '^TestIntegerSIMDProfile$' -v \
  -cpuprofile /private/tmp/integer-cpu.pprof \
  -memprofile /private/tmp/integer-mem.pprof \
  -trace /private/tmp/integer-trace.out -o /private/tmp/integer-profile.test .
go tool pprof -top /private/tmp/integer-cpu.pprof
go tool trace /private/tmp/integer-trace.out
```

Setup verifies Software, TLS and active 4x QPF with six Search workers, `mnp`
routing and 32 connections. The fixture uses driver RESP3 and creates/drops
only `it_integer_simd` in the dedicated database. Do not run concurrent copies
against one database. The fixture is synthetic and includes NULLs and integers
beyond 2^53; each result row, multiplicity, order and int64 physical type is
independently checked. SQL uses `x+0` to retain a CPU residual and fully fetches
all output. Timed SQL also includes harness string rendering and validation;
it is a baseline, not a candidate SQL speedup measurement.

Phase wall time and process-wide allocation deltas are inclusive. The fetch
phase includes Redis/network/TLS wait, protocol parsing and raw-map allocation.
Trace regions expose socket blocking; pprof phase labels expose sampled CPU.
Allocation profiles locate allocations rather than a separate additive time.
Client profiles cannot separately identify server processing and network delay.
Keep raw traces, diagnostics, credentials and endpoints private. Public results
must record source revision, toolchain, hardware, server versions and skips.

## Recorded outcome: no-go for automatic SQL integration

Measured 2026-11-03 (America/Los_Angeles), Go 1.27.3, with three 50 ms trials
per matrix cell. [Complete median matrix](native-summary.json) was generated
by `python3 summarize.py ARTIFACT_ROOT` from the artifacts of
[native CI run 41784862317](https://github.com/redis/redis-adbc-driver/actions/runs/41784862317).
Both runners were native Linux machines with four logical CPUs. Recorded source
is GitHub's PR merge revision `320994aa3510fa883d2f0490f3763125580c7786`
(head `8ebcf23`, base `36aa28d`). Later validation-only changes add explicit
hardware assertions, block-cancellation tests and profile metadata; they do not
change the measured kernels or conversion benchmarks.

Representative medians at 65,536 rows, zero NULLs, 50% selected, in microseconds:

| Hardware | Scalar kernel | Native kernel | Direct map comparison | Conversion + native |
|---|---:|---:|---:|---:|
| AMD EPYC 7763, SSE4.2 | 170.375 | 56.699 | 1,386.274 | 1,558.506 |
| Arm Neoverse-N2, NEON | 136.810 | 52.665 | 1,434.926 | 1,457.236 |

Each architecture covers 36 combinations of size, NULL density and selectivity.
Conversion plus native loses to direct map comparison in all 36 amd64 cases and
35 of 36 arm64 cases. The remaining arm64 cell improves by only 2.2%, within a
small effect range for this short shared-runner experiment. Native kernels can
help resident typed input, but these results do not justify packing decoded maps
for one predicate. At the representative size, conversion increases allocated
bytes per operation from 8,192 to 540,672 and allocations from one to three.

A supplementary Apple M5 Max arm64 run showed 76.672 µs scalar versus 53.767 µs
NEON, but 574.891 µs direct map evaluation versus 743.719 µs conversion plus NEON
for that same cell. It supports the same decision; it is a separate machine/run.

Keep this prototype isolated. Reconsider only if a consumer already owns typed
batches, or several operations can amortize conversion, and independently
validated execute-through-full-fetch measurements demonstrate a benefit. A
faster resident-input kernel alone is insufficient. No SQL query speedup or
universal SIMD crossover is claimed.

## Retesting with PR #297

PR #297's `scanprefetch` build can overlap wide HASH fetches with Arrow
conversion. Its positional Arrow path bypasses the decoded maps used by the
original batch-conversion experiment, so the unchanged kernel microbenchmarks
alone cannot settle the full-pipeline question. A separate consumer experiment
compares direct Arrow row evaluation, copied values plus scalar/SIMD, and SIMD
borrowing existing Arrow values. Validity packing is included in SIMD modes.
This is consumer-side predicate work after a bare-column SQL scan, not a native
SQL WHERE backend or evidence that prefetched SQL automatically uses SIMD.

From `go/`, after the verified Software setup above, run sequentially:

```sh
REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed REDIS_INTEGER_PROFILE=1 \
  REDIS_INTEGER_PROFILE_ROWS=100000 \
  REDIS_INTEGER_WIDE_REPORT=/private/tmp/integer-wide-default.json \
  go test -tags scanprefetchoff -count=1 -run '^TestIntegerSIMDPrefetchProfile$' -v .
REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed REDIS_INTEGER_PROFILE=1 \
  REDIS_INTEGER_PROFILE_ROWS=100000 \
  REDIS_INTEGER_WIDE_REPORT=/private/tmp/integer-wide-prefetch.json \
  go test -tags scanprefetch -count=1 -run '^TestIntegerSIMDPrefetchProfile$' -v .
```

The explicit `scanprefetchoff` tag forces a synchronous baseline on current
default-enabled candidates. At the historical measured revision, the baseline
was already disabled without that tag. The legacy `scanprefetch` tag remains
accepted; it is unnecessary when prefetch is enabled by default.

Each build verifies its actual selected path, excludes one warmup per mode,
rotates four modes over three measured trials and fully validates all 64
columns, NULLs, types, multiplicities and predicate bits outside timing.
Inputs retained for validation remain valid after the reader is released.
The end-to-end interval includes execute, full fetch, predicate computation and
stream shutdown. It excludes fixture creation, connection opening and result
validation. All inputs are retained until validation in both modes; it is a
bounded 100K-row screen, not a full-scale ClickBench claim. Compare modes within
a build; separate build runs are not alternating-process causal prefetch trials.


Measured at `c367645`, including merged main `4735cbd` (#297), on Apple M5 Max
(native darwin/arm64, Go 1.27.3) against the same verified TLS/RESP3 Software
8.2.0-78.18 / Redis 8.6.2 / Search 8.6.11 three-shard 4x-QPF environment.
The clean prefetch run had no concurrent local regression checks. All 24
measured executions and eight excluded warmups passed full independent
validation. Each execution returned 25 batches, initially 4,096 rows.
[Complete sanitized trials](wide-prefetch-results.json). Times in milliseconds:

| Build | Consumer mode | Full-pipeline median | Full-pipeline range | Predicate/preparation median |
|---|---|---:|---:|---:|
| default | row | 2947.027 | 2909.082–2958.208 | 0.192 |
| default | packed-scalar | 2902.691 | 2776.246–2951.554 | 0.452 |
| default | packed-simd | 2804.032 | 2803.898–3168.093 | 0.369 |
| default | borrowed-simd | 3053.656 | 2847.113–3121.690 | 0.299 |
| prefetch | row | 2604.577 | 2549.170–2883.322 | 0.193 |
| prefetch | packed-scalar | 2835.459 | 2682.008–2987.476 | 0.450 |
| prefetch | packed-simd | 2621.518 | 2598.732–2912.576 | 0.387 |
| prefetch | borrowed-simd | 2618.347 | 2544.829–2848.125 | 0.289 |

`packed-scalar`/`packed-simd` copy Arrow int64 values and pack validity before
running the kernel. `borrowed-simd` borrows Arrow int64 values and still packs
validity. `row` reads the existing Arrow arrays directly. This differs from the
original decoded-map experiment and is reported separately.

Prefetch does not establish a SIMD pipeline improvement in this screen. Within
each build, full-pipeline trial ranges overlap, and predicate preparation plus
SIMD takes longer than direct row comparison, even when borrowing values.
Roughly 0.2 ms of predicate work sits inside a roughly 2.5–3.2 second pipeline.
Any lower full-pipeline median for SIMD is far larger than the actual predicate
cost difference and cannot be attributed to SIMD from these noisy trials.
No production SIMD integration or query speedup is justified. Keep the no-go
for automatic map batching; revisit stronger/resident multi-operation workloads
with their own complete-query evidence. These runs do not revalidate #297's
full-scale performance claim or establish a causal prefetch speedup.
