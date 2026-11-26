# Specification

2026-11-25 (base `16032de1`, rebased onto `064eb12d`); see [intent](intent.md), [plan](plan.md), [validation](validation.md).

**Fetch first** (`go/snapshot_flex_candidates.go`, `go/join_star_candidates.go`, #665). With Flex, a parallel star, snowflake or eager-join reader's filter (`flexReaderPlan`) is now evaluated once per source by `fetchCompleteFlexCandidates`.
- **The fetch:** each source's `FT.SEARCH … NOCONTENT LIMIT 0 n` asks for the rows the shared budget (`flexSharedCandidates`, 524,288 rows / 64 MiB) still has room for. Its reply's total is the source's complete count, so there is no separate count.
- **Within the budget:** the reply's keys are the complete candidate set. As before, they must be distinct, valid row IDs equal in number to the total (`completeSnapshotIDsWithin`), pass the source directory proof (`checkCompleteSnapshotDirectory`) and the FT.INFO proofs around each Search, and pass the before/after key checks. The set fills at least two chunks, or the reader keeps its existing plan.
- **Over the budget:** a source whose total the budget refuses ends the read. Its keys, at most the budget's rows, are discarded. Its total alone proves the shared budget refused the set, and so the smaller ordinary budget (`flexScanCandidates`): the reader takes exact-directory ranges, as when the count refused it.
- **Search refuses the fetch size** with its own result limit (a direct native `LIMIT exceeds maximum` with both index proofs): the reader counts first, as before. It then fetches with that count (`openFlexSnapshotCandidatesCounted`) instead of counting a second time.
- **Failures:** a partial reply, an invalid total, or a reply whose keys don't match its total fails the query. It never falls back to ranges or retries.
- **Unchanged:** ordinary serial candidate scans, range-bounded readers such as partitions and directory aggregates, the candidate budgets, the complete residual predicate on every row, exact visibility in every chunk, overlays (whose filters are not pushed to Search), and every RAM plan.

**Cost.** A selective filter now costs one Search evaluation instead of three. An over-budget filter's single fetch also transfers up to 524,288 row keys, which are then discarded; see [validation](validation.md#over-budget-overhead).

**Correlated key demands** (`go/query.go`). A streamed join's fact key demand (`starFactDimensionDemand`) may hold up to `flexDemandTerms` (4,096) keys with Flex, unless the executor runs a correlated subquery (`executor.outer` set, by `subqueryRows`). Such a join keeps `maxUnionTerms` (1,000), as every streamed join did before #644.
- **Why:** a correlated subquery runs once per distinct set of outer values, so its demand's union cost repeats for each, while the outer equality already narrows its fact.
- **Both limits:** `demandTermLimit` sets the limit both for building the demand and for `unionTerm`'s pushdown of a marked demand.
- **Unchanged:** uncorrelated Flex streamed joins, every RAM limit, and other unions. The demand is still part of the fact's complete predicate when built, and the bucket probes still enforce the join when it is not.

Acceptance:
- **Shared path:** `TestParallelStarSharesFlexCandidatesAcrossWorkers` reads 2,000 shared candidates with exactly one Search call, drops the candidate whose row no longer matches, and reads no directory ranges. On main it makes three Search calls.
- **Over budget:** `TestParallelStarProvenOverBudgetFlexUsesOnlyCountThenRanges` still makes one Search call, then reads exact-directory ranges.
- **Short reply:** `TestParallelStarSharedFlexFetchRejectsShortReply`: a fetch whose total exceeds its keys fails, with no rows, no ranges and no HASH reads.
- **Limit refusal:** `TestParallelStarSharedFlexLimitRefusalCountsThenFetches`: with Search's limit below the budget, the refused fetch is followed by one count and one fetch of exactly that count (three calls), with the same rows.
- **Existing tests:** the ambiguous-count, admitted-selective, Flex snapshot candidate and budget tests pass unchanged.
- **Correlated limit (unit):** `TestFlexCorrelatedFactKeyDemandKeepsUnionLimit`. With `outer` set, a Flex executor's limit is `maxUnionTerms`: a 1,500-key demand is not built, and a marked 1,500-term demand is not pushed. Uncorrelated, both happen. It fails without the change.
- **Correlated limit (integration):** `TestSQLFlexCorrelatedJoinKeepsDemandLimit` runs a Q2-shaped correlated minimum on Redis with Flex. 1,360 of 2,000 suppliers are in the region, and `partsupp` is the streamed fact, as at SF1. Its 30 correlated runs push fewer Search unions of over 1,000 terms than there are runs: 4 in all, from the outer join. The rows equal the eager join's. Without the change it pushes 124.
- **SF1 Flex:** Q7 and Q2 fully fetched and validated, with times on main and this change, and the full suite on both.
