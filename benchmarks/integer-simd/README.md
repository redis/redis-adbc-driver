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

The `Integer SIMD experiment` workflow runs these measurements on native hosted
amd64 and arm64 hardware. Emulation/cross-compilation is not hardware validation.
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
