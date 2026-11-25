# Plan

2026-11-25; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Extract `parallelTableRows` and `parallelTableEligible` from `scanParallelDimension`. Read large Flex eager-join inputs through them in `scanJoinItem`, and record the worker grant.
2. Let `partitionedAggregate` run on Flex managed tables, with exact-directory partition scans and the complete predicate as the residual, and no Search tuples, compact probes or visibility queries.
3. Add the two integration tests; confirm they fail on main and pass after.
4. Trace subset Q1, find the `distinctResidual` refusal of Q1's interval filter, and skip that tuple-only check for Flex. Extend the test with Q1's filter.
5. Run the unit suite, the ordinary integration selection, all four snapshot shards on a dedicated 4x QPF fixture, and the subset in Flex and RAM modes against DuckDB.
6. Run SF1 Q1 and Q19 on Flex in a coordinated read-only window, then open the PR.
