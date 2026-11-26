# Plan

2026-11-25 (base `16032de1`, rebased onto `064eb12d`); see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Provision SF1 Flex like #643–#666's runs, with a driver built from the newest main (`16032de1`), and load TPC-H SF1 through it.
2. Probe the Search cost of Flex key unions, read-only, to test #665's option of a larger key demand. It costs far more than it saves, so it was rejected.
3. Reuse the reader plan's counts for the shared fetch. Then fetch first, with the count taken from the fetch's reply.
4. Update the shared-candidate unit test, and add the short-reply and limit-refusal cases.
5. Run Q7 interleaved on main, count reuse and fetch first. Measure the over-budget fetch's overhead.
6. Run the full SF1 Flex suite on main (#648). Diagnose Q2's timeout: compare it with main `fdd49f7c`, then read the server's slow Search calls.
7. Keep `maxUnionTerms` for correlated streamed joins. Add the unit and integration tests, and confirm on SF1 Q2.
8. Rebase onto main `064eb12d`. Run the full suite on this change and on that main.
9. Run the unit suite, the targeted Flex/Snowflake/Star/Candidate/Subquery integration tests and the snapshot suites, then open the PR.
