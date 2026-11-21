# Contract

Date: 2026-11-20. Source: `1b5918e6`. Status: locally verified narrow implementation of
[#564](https://github.com/redis/redis-adbc-driver/issues/564); see [intent](intent.md)
and [plan](plan.md).

An append/create_append may create its final SKIPINITIALSCAN index before input
consumption only when it owns a fresh private segment, its exact directory is
empty, every public column is NOINDEX, no hidden provenance TAG is present, and
it is not a retained operation. Therefore the index layout cannot change from
public NaN/TAG observations. Other paths retain existing behavior.

The existing guarded Search command checks writer admission before and after
creation. Failure or uncertain reply aborts without retrying creation or input.
Keep deferred staging state (authoritative directory reads) throughout input;
mark only successful early creation in memory. No durable format changes are
needed because retained operation preparation is excluded. At sealing restore
the final overlay flag, retain the same index identity and skip its creation and
directory notification pass. Then run the existing native readiness, zero
failure, exact prefix/cardinality and warning-free indexed row-ID count proofs.
Missing/rebuilding early indexes fail publication; never repair by replaying rows.
The existing typed query-permission fallback remains unchanged.

Atomic complete-image batches, durable unknown-row-outcome resolution, append
leases/CAS rebases, pinned readers and reference-safe abort/GC remain unchanged.
Tests must demonstrate index-before-rows and no final notification, eligibility
fallbacks, publication proof errors and failed input isolation; existing parallel
append/cancellation regressions continue to apply. Native evidence requires
Redis Software with verified QPF4, including Flex. Performance claims require
validated comparable 105-column serial/four-writer runs and phase/resource data;
small correctness fixtures alone cannot establish a speedup.
