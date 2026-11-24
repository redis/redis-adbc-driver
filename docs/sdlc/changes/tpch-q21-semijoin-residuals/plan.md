# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Reproduce Q21 on the 10% TPC-H subset (isolated Software fixture, verified 4×
   QPF) with the #630 library and record outer rows, commands and bytes.
2. Locate where Q21's subqueries are evaluated: not movable into the join
   (`joinPlan.itemsOf`), rejected by star streaming (`planStarShape`), so the
   residual runs in `scanMem` over `runJoin`'s rows.
3. Write `TestSQLSemiJoinResiduals` (new file) and confirm it fails on the base.
4. Add residual correlation to `go/semijoin.go` (planning, group rows,
   evaluation) and the page-probe hook to `scanMem` in `go/query.go`.
5. Update `TestSQLAliasScopeSubqueryPaths`, which asserted the old per-row path for
   `x.k = e.k AND x.v > e.v`. Keep its per-distinct-value check with `e.v + 0`.
6. Run the semi-join/subquery selection, every integration test that uses EXISTS
   or subquery work counts, `gofmt`, and the unit-only pass.
7. Re-measure Q21 on the subset; record the probe cost breakdown. SF1 is
   verified separately before closing #587.

Follow-up candidate (not in this change): run a page's probe batches
concurrently under the worker budget. A microbenchmark scaled almost linearly to
four connections, but executor copies share DML/retained-execution state that
needs its own review.

No merge, release or human review approval is inferred.
