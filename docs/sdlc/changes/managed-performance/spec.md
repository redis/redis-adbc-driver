# Contract

See [intent](intent.md), [plan](plan.md).

- Reuse the existing immutable directory/overlay reader for eligible committed
  single-generation, row-local SELECTs without explicit ordering or LIMIT.
  Preserve metadata/index readiness validation, reader pins/leases, exact physical
  identities, residual predicates, tombstones, projection and Arrow cleanup.
  Keep private outputs, unmanaged/imported layouts and complex operators on their
  established paths. Bound single-directory positional reads to the existing
  4,096-row HASH pipeline ceiling; multi-segment/compaction pages stay unchanged.
  No stored descriptor or protocol change.
- Do not weaken source or index ACLs, late NULL/default/provenance behavior,
  dependency validation, cancellation or unknown-outcome handling.
- Reduce directory-merge scratch allocations and generic formatting where the
  stored representation is already known. Compute identical batch byte budgets;
  preserve the 256-row/1 MiB target and 64 MiB single-row ceiling.
- Keep managed row batches serialized around their single durable outcome slot.
  Preserve all final indexing/readiness and atomic publication checks.
- Validate both exact results and lifecycle under verified Software 4x QPF.
  Report client microbenchmarks separately from end-to-end observations; retain
  historical results with their actual server/settings.
