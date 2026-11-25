# Plan

2026-11-25; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Profile subset Q9 (CPU and allocations) on main through a scratch `go test -overlay` runner
   that fully reads the Arrow stream.
2. Implement the four changes in the spec; add the unit tests and show the allocation test fails
   with main's `value.go` / `eval.go`.
3. Re-profile Q9 and compare bytes, mallocs and GC CPU per run.
4. A/B TPC-H Q1/Q3/Q5/Q6/Q7/Q8/Q9/Q10/Q14/Q18/Q19 against main in fresh processes, validated
   against DuckDB.
5. Run the unit-only pass and the snowflake/star, decimal and snapshot integration selections.
6. Open a PR for #652 with the evidence and what remains.
