# Plan

2026-11-25; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Load the 10% TPC-H subset (orders with `o_orderkey % 10 = 1` plus their lineitems, full
   dimension tables) into an isolated Redis Software fixture with verified 4× QPF.
2. Time raw `FT.SEARCH` unions of 1–4,000 terms (count, sorted page, row-ID window,
   unsorted) against a range, and the driver's `IN` lookups against a range.
3. Add `TestSnapshotCandidateBoundedWindows/large_union`; show it fails on main.
4. Add the complete read to `go/snapshot_candidates.go` (see spec).
5. Run the candidate, union, join, semi-join and subquery selections and the unit-only pass.
6. A/B the #631 lookup shape and TPC-H Q3/Q4/Q5/Q7/Q8/Q18/Q19/Q21 on the subset in fresh
   processes, fully fetched and validated against DuckDB 2.0.1.
7. Open a PR that fixes #631 and post the measurements there.
