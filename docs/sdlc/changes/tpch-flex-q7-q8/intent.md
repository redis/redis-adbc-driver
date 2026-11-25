# Intent

Requested 2026-11-25 by the user, relayed by the coordinating session: "Yes, try Q7/Q8". Lift Flex's candidate-row cap and Search union-term limit so that TPC-H Q7 and Q8 read tens or hundreds of thousands of lineitem rows instead of all 6M. Stacked on the [Flex read-path change](../tpch-flex-read-path/intent.md) (#643). See [spec](spec.md), [plan](plan.md), [validation](validation.md).

On SF1 Flex, every full lineitem scan is bound by the single shard's cold flash reads; see [#643's validation](../tpch-flex-read-path/validation.md#sf1-flex). Q7 and Q8 are the two queries that a bounded candidate set could narrow:
- Q7's lineitem filter (ship-date window plus the 798-supplier key union) has about 146k candidates, just over the 131,072-row Flex candidate budget.
- Q8's 1,333 part keys exceed the 1,000-term union pushdown limit, so its lineitem demand was never pushed to Search.

Scope, as relayed:
- Flex-only and bounded. RAM behavior and the existing ordinary caps are unchanged, and #631's per-term union cost is not reintroduced on RAM.
- The complete SQL predicate stays the residual, with exact visibility.
- Add a focused test that fails before the change, check the 10% subset in Flex mode against DuckDB, and run Q7 and Q8 on SF1 Flex with full fetch and validation, saying plainly if they miss 60 s.
