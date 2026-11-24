# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Reproduce on a 10% TPC-H subset (every order with `o_orderkey % 10 = 1` and
   its lineitems, full dimension tables) in an isolated Redis Software fixture with
   verified 4× QPF; record plan kind, commands and allocation per query.
2. Admit exact decimal `SUM`/`AVG` inputs in `go/partitioned_aggregate.go` for
   client partial groups only; encode, merge and finalize exact partial sums.
3. Add `go/partitioned_decimal_integration_test.go`: failing-before shape test,
   serial equivalence with types/scales across two forced ranges, HAVING, NULL-only
   groups, zero sums, mixed scales, integer×decimal, and rejected shapes.
4. Run existing partitioned/aggregate/subquery integration tests and the
   unit-only package pass.
5. Re-measure Q1/Q18 on the subset; SF1 verification on the issue's profile is
   tracked separately before any issue is closed.

No merge, release or human review approval is inferred.
