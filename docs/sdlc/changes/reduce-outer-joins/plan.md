# Plan

2026-11-26 (base main `5adb8027`); see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. From the warm-up diagnostics of the SF1 Flex failures, identify the sales-`LEFT JOIN`-returns queries whose `WHERE` rejects NULLs (Q93, Q49).
2. Add `outer_join_reduce_test.go` on the in-memory TPC-H-shaped fixture: plan cases that must and must not reduce, result equality with and without the reduction, and streamed-join admission of a Q93-shaped query. Run it before the rule exists, and record the failures.
3. Implement `reduceOuterJoins` and `strictIn` in `join.go`, and call it in `planSelect` before `planPushdown`.
4. Run the unit-only suite.
5. On a TPC-DS SF1 Software Flex fixture with verified 4x QPF, run Q93 and Q49 alone, and compare with the main library's diagnostic run.
6. Run all 99 TPC-DS queries at SF 0.1 on the same fixture, each validated against DuckDB.
7. Report the CI integration-suite results on the PR.
