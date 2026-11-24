# Specification

Date: 2026-11-23. Source: `07b24edb`. Status: local candidate.
See [intent](intent.md) and [plan](plan.md).

Only pinned, immutable, driver-owned, managed plain-index Flex query scans with
an already compiled nontrivial source predicate are eligible. Ordinary scans,
private writer output, unsupported/unmanaged/live layouts, pending cleanup,
and sources lacking a complete descriptor retain the directory path unchanged.
Do not change `snapshotIndexable` or compile a new source predicate here.

Exact directory merge advances all versions before any filtering. Read and
validate row-ID/deletion controls of every winner, including nonmatches. Missing
or mismatched authoritative HASHes fail. Tombstones suppress older matches. Query
each surviving source in batches of at most 256 INKEYS; no SORTBY or cursor.
Validate HASH prefix identity, full published document count, zero indexing
failures and complete readiness before/after Search. Reject warning, truncated,
duplicate or out-of-batch replies, all pipeline/individual errors, and lease
failure. Preserve original directory order and refill entirely filtered pages.
Search is only a membership test; original SQL residual and physical field
mapping stay intact. No uncertain capability/transport error becomes fallback.

Scalar aggregate directory partitions retain only their already compiled source
query and full SQL predicate. The exact int64 directory bounds remain separate;
synthetic numeric worker ranges are never added to Flex Search queries.

Metadata format and public API are unchanged. Real Redis Software behavior,
active 4x QPF, complete result validation, and timings remain live validation
requirements. Pure mocked-transport tests cannot establish those claims.

## Final production route

This record describes the preceding page-filter implementation and its unit-tested helpers. The production query route in this PR is superseded by [complete bounded candidates](../flex-complete-candidates/spec.md), which reuses these conservative eligibility/index-proof helpers. Broad or unsupported scans retain the original exact-directory path **without** per-page membership probes. The standalone page-filter primitive remains covered by focused tests; it is not installed as the production fallback.
