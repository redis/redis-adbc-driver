# Specification: host-staged GPU experiment

Date: 2026-11-03. Source: `95d6583`. Status: experimental.
See [intent](intent.md), [plan](plan.md) and
[detailed feasibility assessment](../../../gpu-query-feasibility.md).

Use existing host Arrow output with optional consumer-side cuDF execution.
Preserve the shipping CPU default. The experiment supports nullable int64
group keys/values, a nonnegative-value predicate and count/min/max/sum.
Reject out-of-domain types and overflowing sums before GPU execution. Compare
all results and output physical types with an independent Python oracle.

Time upload, synchronized compute, download and host-to-host execution. Redis
mode reads an existing stable table, fully fetches both scan and direct SQL,
and clearly labels modeled pipeline costs. Synthetic trials cannot establish
Redis query speedup. CUDA failures cannot silently run on CPU.

Native device output is deferred: the pinned ADBC C boundary has no device
entry points, and arrow-go's async device handler accepts CPU producers only.
A native GPU executor requires SQL exactness, admission/cost selection,
cancellation, bounded memory and lifecycle work beyond this experiment.
