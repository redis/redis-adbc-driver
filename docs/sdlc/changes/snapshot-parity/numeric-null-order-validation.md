# Visibility-proven numeric NULL ordering

Date: 2026-11-04. Parent f67806d. Storage protocol remains 15.
Full [parity](plan.md) remains incomplete.

The [controlled pre-snapshot comparison](pre-snapshot-read-validation.md) found
567.717 ms snapshot versus 84.206 ms mutable default descending top-K. Explicit
NULLS LAST on the same non-NULL values used the index and took 14.421 ms. The
planner unconditionally rejected NULLS FIRST for nullable indexed columns.
A focused real replacement-generation regression first failed with both indexed
ordering and pushed limit false (subtest/parent fail, root 1.034 s).

The planner now admits nullable numeric NULLS FIRST ordering after proving that
the pinned Search candidate superset has no NULLs. It reuses exact immutable
visibility masks, including replacement directories and tombstones. Each source
must have ready, warning-free metadata, exact registered image cardinality, its
registered prefix and zero indexing failures. A complete FT.SEARCH count excludes
all present numeric values; only zero missing visible candidates permits the
ordering. The existing rounded-value repair and global OFFSET/LIMIT logic remain.
A baseline uses the same proof through a local metadata copy. Cached masks live
only in this preparation; no persistent NULL facts, format change or migration
are introduced.

All ordering keys pass metadata eligibility before optional proof requests run.
Empty predicates, direct key lookups and unsortable order expressions skip those
requests. Strings, virtual missing defaults, unsafe numeric row-ID bounds,
excess masks, disabled pushdown and populated unsealed outputs retain their
conservative paths. NULLs outside the pushed candidate predicate do not prevent
the proof; residual SQL remains evaluated by the existing plan, without replay.

Denied or partial optional count replies preserve a complete exact scan with no
pushed ordering/limit. Directory/metadata errors, rebuilding/incomplete indexes,
partial FT.INFO fields and cancellation refuse admission with typed errors.
FT.INFO field lists and maps are normalized before warning checks. The initial
mandatory descriptor validation stays intact; fault fixtures activate only after
that validation to exercise the new boundary. One intermediate guard test was
too strict about full unbounded index reads; the final assertion specifically
prohibits unproved SORTBY/LIMIT while preserving the existing complete fallback.

## Validation and review

Owned strict-TLS Redis Software, noeviction and active effective 4x QPF verified
before tests. Database-mutating selections sequential.

- Final focused matrix: **18 race test events pass, zero test skips/failures;
  root 5.150 s**. Includes both directions with ASC NULLS FIRST/OFFSET, values
  above 2^53, replaced/deleted NULLs, one baseline, new concurrent NULLs, NULLs
  outside the candidate predicate, old NULL/non-NULL readers through GC,
  denied/partial count replies, building/incomplete/denied/partial metadata,
  cancellation, wide physical IDs, strings, unsealed private output and absence
  of unnecessary probes.
- Broader ordering/NaN selection: **45 race test events pass, zero test
  skips/failures; root 62.207 s**. Includes existing rounded-value and managed-copy
  ordering. The final explicit ascending assertions above ran separately after
  this selection; production planner code was unchanged between them.
- Unit-only packages: **957 pass events, 887 expected integration skips, zero
  failures; root 26.590 s**. Redis environment variables unset. Unit skips do not
  establish server compatibility.
- Versioned native make build passes. The [native comparison](numeric-null-order-performance.md)
  records complete validated before/after and mutable-baseline measurements.
- Formatting and diff whitespace checks pass. CUDA has no applicable selected
  tests in the integration selections and is skipped.

Reviewed visibility before predicates/order/limit, exact numeric ties and NULLs,
per-generation proof lifetime, index/directory/prefix ownership, error/refusal
paths, private index exclusion, residual evaluation once, cancellation and old
reader retention. No writer, protocol, ACL grant or replay change. Author review
is not independent maintainer approval. A complete current full ordinary run,
current-protocol Cluster/role/recovery and native/platform coverage, process/input
resume, sustained compaction and broader comparative performance remain open.
