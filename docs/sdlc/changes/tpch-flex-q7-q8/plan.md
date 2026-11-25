# Plan

2026-11-25; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Let the Flex fact key demand exceed `maxUnionTerms`, marked and bounded (`flexDemandTerms`).
2. Measure SF1 Q8 with only that change. Rows read fell from about 7.7M to 1.71M, but the candidate fetch was serial and cold flash reads are slow per connection.
3. Parametrize the Flex candidate budget, and add shared complete-candidate materialization split across a parallel reader's workers, for facts and large dimensions.
4. Add the failing-before unit and integration tests, and update the two policy tests.
5. Validate on the subset in Flex and RAM modes against DuckDB.
6. Run SF1 Q7 and Q8 on Flex, read-only, with full fetch and validation, then open a PR stacked on #643.
