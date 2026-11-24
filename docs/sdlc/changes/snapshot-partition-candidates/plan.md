# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. On a dedicated Software fixture with verified 4× QPF, measure three scratch builds
   with interleaved third-path Q19 rounds (PR #22 scanner, `partition_count` =
   `scan_threads`):
   - base;
   - 4,096-row directory pages with a residual;
   - span-bounded Search candidates per partition.

   Record concurrent host load.
2. Keep only what clearly helps: filtered partitions read Search candidates.
   Restrict the change to predicates that add index terms, because the prototype also
   moved unfiltered `part` partitions onto a slower pure row-ID range.
3. Add `go/snapshot_partition_candidates_integration_test.go`; show it fails on the
   original `snapshot_partitions.go` and passes after.
4. Change `go/snapshot_partitions.go` (see spec) and update the partition-bounds
   sentence in `docs/snapshots.md`.
5. Rebuild the traced Linux library and repeat the interleaved matrix:
   base, fix and prototype.
6. Run targeted partition/snapshot integration tests, the snapshot partition suite
   on a disposable database, and the unit-only pass.
7. Open a PR referencing #599. No release or review approval is inferred.
