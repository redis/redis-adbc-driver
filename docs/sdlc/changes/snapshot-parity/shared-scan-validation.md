# Shared-scan pinning, exact visibility and lifetime

Date: 2026-11-04. Parent 7ec179e. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh ordinary continuation failed in TestSharedScanExactResults and its
first child on an unpinned planner catalog read (root 102.576 s). Shared-scan
fixtures now acquire one statement snapshot before planning and retain it through
readiness, execution, complete Arrow fetch and comparison. Hooks are installed
before clients are shared; measurement does not replace a store beneath its pin.

Native segmented reads after ALTER, replacement UPDATEs, NULL updates and DELETE
compare five query shapes with both an explicit exact-directory/full-predicate
reference and independently calculated expected rows. Cases include global
LIMIT/OFFSET, sparse insertion order, decimals, NULLs, COALESCE, empty results and
bound integer arithmetic above 2^53. Stream schemas and 31-row output bounds stay
checked. Source assertions require native visibility; the reference must use
directories rather than accidentally reuse the same Search candidate path.

Range/cursor primitives operate on a pinned clone of one immutable physical
source, leaving published metadata untouched. They retain unrestricted-cap sort
bounds, selective Search terms, direct-key lookup and unsafe-layout/global-operator
fallback guards. Sequence-boundary metadata is published privately: an old pin
retains its original bounds while a fresh plan refuses inexact Search ranges.
The 25,000-row multi-page test requires a live Search cursor, checks all exact
rows/order across ranges and proves cursor deletion after early release.

Native reader lifetime retains release/cancellation and bounded pending output.
TRUNCATE, INSERT, DROP and recreation plus GC cannot change a started stream's
complete original 2,500-row result. Source directories remain intact until the
reader pin releases, then rows/indexes/directories/reference records must retire.
The recreated public binding is empty and an empty native query returns no stream.

## Validation and review

Owned strict-TLS Redis Software, noeviction and effective active 4x QPF verified
before testing. All 17 focused race test events pass, zero test skips/failures;
root 10.994 s. An earlier version also passed all 17 events (root 11.006 s)
before strengthening the explicit directory reference/source counters. Mutating
selections ran sequentially. The CUDA package has no applicable selected tests
and is skipped. Compilation, formatting and diff whitespace checks pass.

The diagnostic 5,000-row single-source primitive with an intentionally forced
137-row cap used 37 Search aggregates; its reference used 20 directory pages.
Both independently validate every ordered ID. Counters reset after planning and
are read under their mutex. Single-run elapsed logs on this artificial cap are
not a controlled comparative benchmark or evidence of full performance parity.

Reviewed pin/context lifetime, unchanged published input, independent complete
results, global predicates/limits/order, exact integer/decimal/NULL behavior,
explicit native versus primitive sources, release/cancellation, current versus
retained bindings, cursor deletion and reference-aware reclamation. No production,
protocol or ACL changes. Author review is not independent maintainer approval.
Full ordinary/platform, current-protocol Cluster recovery, restricted-reader,
public process/input resumption, sustained compaction and comparative performance
gates remain open.
