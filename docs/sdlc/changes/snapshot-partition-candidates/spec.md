# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

`snapshotPartitionReader` (`go/snapshot_partitions.go`) still plans the statement with
the caller's predicate AND the exact span bound `__rowid >= lo AND __rowid <= hi`. It
forces the snapshot overlay and sets the request's row-ID ranges as before. It then
chooses one of two reads.

**Search candidates.** Chosen when `snapshotPartitionFiltered` holds. All of these must
be true:

- The plan has no direct row keys, is not `none`, and has a Search query other than `*`.
- Both span conjuncts plan as exact row-ID terms (`comparisonTerm` with no recheck).
  Bounds at or above 2^53 are inexact and fail this.
- The planned query ends with exactly those span terms, and has other terms in front of
  them. `planWhere` joins conjunct terms in order, and the span bound is the last
  conjunct, so the predicate contributed index terms.
- `snapshotIndexable` holds for the overlay.

The read keeps the planned query and the planner's residual. It sets `original` to the
caller's predicate. The result is the same request an unpartitioned filtered snapshot
scan uses, restricted to the span by exact Search range terms:

- `snapshotCandidateScan` keeps newer-directory visibility, identity checks, tombstones,
  partial-result refusal and #634's bounded windows.
- With no residual, the existing positional HMGET fetch and 4,096-row pages apply.
- If the index is unusable when the scan opens, `openScan` falls back to exact
  directories. That fallback seeks the request's ranges and evaluates `original`.

**Exact directories.** Used in every other case; the code is unchanged. The query becomes
`*`, and the residual and `original` become the caller's predicate. This covers:

- unfiltered partitions (the span bound alone also plans as a Search range, and a pure
  row-ID range reads faster from directories);
- predicates with no index terms;
- `__rowid = N` lookups;
- inexact bounds;
- unindexable overlays.

Partition production, descriptors, pins, leases and ordering promises (none) are unchanged.

Acceptance:
- A regression test reads filtered partitions on another connection with a hooked
  transport. It fails before the change: 0 of 4 filtered partitions read span-bounded
  candidates. It passes after.
- In that test, unfiltered and non-indexable partitions issue no row-ID Search queries.
- Rows equal the unpartitioned result in every case, including bound parameters, a
  residual, NULL values, UPDATE visibility (stops matching, starts matching, newer
  values) and a DELETE.
- Existing partition and snapshot tests pass.
- On an interleaved third-path Q19 matrix, the lineitem partition read is faster than base.
