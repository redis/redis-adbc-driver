# Specification

Date: 2026-11-03. Baseline: `36aa28d`. Status: experimental internal package.
See [intent](intent.md) and [plan](plan.md).

`internal/intpredicate` accepts decoded signed int64 values, an optional packed
validity bitmap, an int64 literal and one of six comparisons. It produces an
owned packed match bitmap in input order. Bit i represents row i; NULL and
padding bits are zero. No integer subtraction, float conversion, coercion,
reduction or SQL expression evaluation is permitted. Shape errors precede work;
cancellation returns no partial result and is checked around 65,536-row blocks.

Runtime dispatch requires SSE4.2 on amd64 or ASIMD on arm64. Native C intrinsics
run only after the feature check. Other CPUs/platforms, `CGO_ENABLED=0`, and
`-tags purego` use the Go reference. C borrows slices for the synchronous call
and retains no pointers. No external libraries or experimental Go flags are used.

No production caller is added. Eager decoding or arithmetic across rows can
change the first error; a mask-only primitive is insufficient authorization to
replace the SQL DAG. Decimal/floating reducers and Arrow ownership stay untouched.
CUDA's byte validity ABI stays separate; a shared representation needs its own
conversion/lifetime and end-to-end cost evidence.

Benchmarks compare scalar/native kernels and map conversion plus each kernel
against a direct map comparison. Sizes: 64, 1,024, 65,536; selected proportions:
0/1/50/100 percent; NULL proportions: 0/50/100 percent. These proportions refer
to generated input before NULL masking; correlated NULL placement is deliberate
and not a random-distribution study. All benchmarks include owned output allocation.

The integration profile records fetch (including TLS/protocol/Redis wait),
decode, typed conversion, kernel, residual and execute/full-fetch phases.
Allocation bytes/counts accompany wall times; pprof labels and runtime trace
regions allow CPU and blocked-network inspection without attributing waits to
SIMD. Client traces do not distinguish server execution from network latency.
All fetched rows and physical types must match an independent fixture oracle.
