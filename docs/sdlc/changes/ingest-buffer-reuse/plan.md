# Plan

1. Inspect synchronous insertion ownership and existing slice/error boundaries.
2. Add a small execution-local conversion buffer and deferred clearing in the
   unchanged conversion/write closure, retaining only existing bounded slices.
3. Cover growth/tails/empty rows, string/decimal/record reference clearing and
   reuse after errors; retain existing generic conversion parity tests.
4. Run focused unit/race and Software managed/unmanaged ingestion regressions
   only when the parent grants the serialized heavy slot. Verify Software active
   4x QPF and strict TLS/RESP3 through the parent before live access.
5. Freeze source before allocation/native timing. Compare full repeated conversion
   workloads with fresh allocation and reuse, preserving all samples; parent runs
   exact baseline/candidate native ingestion with independent complete validation.
6. Review lifecycle/privacy/semantic evidence, then request parent review before
   any push or PR. No merge is authorized by this record.

See [intent](intent.md), [contract](spec.md) and [validation](validation.md).

Outcome: evidence only. The original frozen contract remains above; measured
source `55396058` is preserved unapplied. Native memory gains accompany mixed
elapsed outcomes, so the original adoption gate is not established. Production
Go remains identical to `88774f10`. See [validation](validation.md).
