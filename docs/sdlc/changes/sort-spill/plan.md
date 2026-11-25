# Plan

2026-11-25; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Split `selectScanned` into its scan and `selectRows` (accepting precomputed keys).
2. Add `sort_spill.go`: eligibility, buffering, sorted runs with an in-memory type/name table,
   multi-pass merge and the streamed row source; make `streamReader.union` a
   `streamRowSource` interface; call `sortSpillSelect` from `streamSelect`.
3. Add `TestSQLSortSpill` (forced 16 KiB budget against the default in-memory path).
4. Run the unit-only pass and the ORDER BY / stream / union / LIMIT / window selection.
5. Measure peak RSS of a full `lineitem` sort on the subset against main, validated against
   DuckDB.
6. Open a PR as part of #654; the join half stays open there.
