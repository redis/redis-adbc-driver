# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Reproduce Q4 on the 10% TPC-H subset (isolated Software fixture, verified 4×
   QPF) and record outer rows, commands and bytes.
2. Add `semiProbeSubqueries`, `semiRowKey`, `probeSemiJoin` and probed-key answers
   to `go/semijoin.go`; add the page hook in `scanIter.keep` (`go/query.go`).
3. Measure probe batch cost by union size; bound batches at 256 keys.
4. Update `TestSQLSemiJoins` for the new work counts and add the multi-batch case;
   run the subquery/semi-join integration selection and the unit-only pass.
5. Re-measure Q4 on the subset; SF1 verification is tracked separately before
   closing #580.

No merge, release or human review approval is inferred.
