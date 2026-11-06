# Pruned partition snapshot lifetime

Date: 2026-11-04. Parent 9047223. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The next ordinary partition selection initially passes 18 test events and fails
TestPrunedPartitionReads: its legacy assertion expects UPDATE/DELETE to change an
already produced descriptor. The driver correctly retains original values 4998
and 4999. The fixture now requires those original rows, checks fresh SQL sees
9000 and 5001, and preserves exact pruning comparisons for all thirteen predicate
shapes. Completed groups are explicitly released rather than consuming lease
capacity until expiry.

Small Arrow batches still exercise cancellation and connection Close. An active
reader and a fresh replay of its descriptor must retain the independently fetched
original result after TRUNCATE, DROP and GC. Releasing both generations must
reclaim their private HASHes, indexes, directories and ownership records; new
reads fail with the existing expired-or-released I/O diagnostic. No production
code or descriptor format changes are needed for this checkpoint.

Verified on owned Redis Software with active effective 4x QPF and noeviction;
mutating selections run sequentially:

- Final focused race selection (PrunedPartitionReads, PartitionPredicateBounds,
  PartitionPredicateBudgets): 38 passed, zero skips/failures; root 15.146 s.
- Earlier ordinary continuation: 43 passed, one failure, root 14.374 s. Pruned
  reads and all predicate cases pass; the next failure is
  TestBoundedScanRejectsBuildingIndex, whose sharedScanPlan helper reads catalog
  metadata without a statement snapshot. Later selected cases did not run.
- Formatting and diff whitespace checks pass.

The permission-specific case is excluded from these selections because production
restricted-reader integration remains open; it is not counted as passing. Full
ordinary/platform, current-protocol Cluster recovery, process/input resume and
sustained/comparative performance gates remain open. Disjoint selections do not
constitute a complete ordinary-suite pass.
