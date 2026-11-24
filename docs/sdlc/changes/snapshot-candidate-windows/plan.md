# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Reproduce third-path Q19 with the released scanner (`98ff0a39fc`, DuckDB 2.0.1)
   on an isolated Redis Software fixture; record the exact SQL the scanner sends.
2. Measure direct ADBC reads of the same SQL (literal and bound parameters), filtered
   scan scaling, and single `FT.SEARCH` page latency by keyset position.
3. Change `go/snapshot_candidates.go` to bounded row-ID windows with an open-ended
   first page and tail page (see spec).
4. Add `go/snapshot_candidate_windows_integration_test.go`; show it fails on the
   original file and passes after. Run candidate/prefetch tests, the unit-only pass
   and the Software suite.
5. Measure SF1 scanner-shaped scans and third-path Q19 before/after on a dedicated
   Software fixture with verified 4× QPF; validate fully fetched results.
6. Open a PR referencing #599 (and #627 for the same scan path); post measurements on
   #599. No release or review approval is inferred.
