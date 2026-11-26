# Specification

Date: 2026-11-25. Baseline: `c8f6f50d`. Issue: [#627](https://github.com/redis/redis-adbc-driver/issues/627).
See [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Contract

- `snapshotIndexable` keeps its meaning: sorted Search candidate and visibility-query reads, never on Flex. It is now that Flex exclusion plus `snapshotSearchable`, which holds the remaining checks:
  - the table is an overlay,
  - no segment is a nonempty owned private segment,
  - the row-ID sequence is below 2^53.
- `snapshotCountQueries` uses `snapshotSearchable` and builds the per-segment visibility queries that `snapshotIndexQueries` builds, sharing `snapshotVisibilityQueries`: NUMERIC row-ID ranges masking shadowed versions, and `-@__adbc_snapshot_deleted:[1 1]`.
- Approximate `GetStatistics` row counts call `snapshotCountQueries`, so on Flex they count with Search like RAM databases. Everything else is unchanged:
  - range readiness checks,
  - the guard that a count never exceeds the segment's rows,
  - the fallback to authoritative directories when masks are inexact or excessive, or a private segment is unsettled.
- Every scan, aggregate, partition, null-order and DISTINCT caller still calls `snapshotIndexQueries` or `snapshotIndexable`. So Flex reads still never use the sorted Search candidate path, and the complete-candidate join readers (#689) are unchanged.
- Exact (`approximate=false`) statistics are unchanged.

## Evidence for using counts but not candidates (Software 8.2.6-58)

- **Counts:** on a flash-backed 288,464-row segment, Search counts of the visibility queries matched exact expectations, for every row, and with 10 or 1,475 IDs masked.
- **Sorted candidate paging:** `SORTBY __rowid ASC` keyset paging returned every ID in order. It took 47 s, against about 4 s for the directory read, and its first page needs `TIMEOUT 0`.
- **Still rejected:** `SORTABLE`, `WITHCURSOR`, `FT.ALTER` and `FT.DROPINDEX … DD`.

## Risks

Approximate statistics on Flex now depend on FT.SEARCH access and index readiness, as they already do on RAM databases. Flex publication already relies on the same NUMERIC counts. Counts on segments with masks are tested after UPDATE and DELETE, and after compaction.
