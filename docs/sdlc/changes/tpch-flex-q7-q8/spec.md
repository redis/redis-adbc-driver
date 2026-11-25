# Specification

2026-11-25; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

**Flex fact key demand** (`go/star_fact_demand.go`, `go/query.go`, `go/sql.go`). A streamed join's fact key demand is the OR of a dimension's distinct join keys. With Flex, it may hold up to `flexDemandTerms` (4,096) keys instead of `maxUnionTerms` (1,000).
- The demand marks its root OR (`Binary.keyDemand`), and only that marked OR may exceed `maxUnionTerms` in `unionTerm`, and only with Flex.
- Every other OR or IN list, and every union without Flex, keeps `maxUnionTerms`. RAM therefore pays no new per-term cost (#631).
- The demand remains part of the fact's complete predicate, which is checked on every row.

**Shared complete candidates** (`go/join_star_candidates.go`, `go/snapshot_flex_budget.go`, `go/snapshot_candidates.go`). A parallel star or snowflake reader (`scanParallelStarFacts`, `scanParallelDimension`) with a Flex Search filter counts that filter's complete candidates once.
- **Shared path:** if the set is within `flexSharedCandidates` (524,288 rows and 64 MiB of keys) and fills at least two 256-row chunks, the coordinator materializes it once (`openFlexSnapshotCandidatesWithin`). That includes the existing Search and directory proofs. The merged candidate row IDs are divided into 256-ID chunks, and each worker scans one chunk through a copy of the candidate scan restricted to that row-ID range (`snapshotCandidateScan.split`).
- **Per-chunk checks:** each copy still resolves newest-version visibility against newer directories, rejects missing or mismatched rows, skips tombstones, checks the complete predicate on every row and verifies directory cardinalities at its end.
- **Order:** facts deliver chunks in row-ID order through the existing ordered scheduler, and dimensions keep chunk order.
- **Over the ordinary budget:** if `flexScanCandidates` (131,072 rows / 16 MiB, unchanged) refuses the set, the reader uses exact-directory ranges as before. That includes a set the shared materialization later refuses. Dimensions now take those ranges too, instead of their serial whole-table read.
- **Otherwise:** the reader keeps its existing plan.
- **Unchanged:** ordinary scans, and range-bounded readers such as partitions and directory aggregates, keep `flexScanCandidates`. RAM databases have no plain Flex index, so they never count, and keep every plan.

**Limits observed.** After DML, a Flex overlay's filters are not pushed to Search, so the shared path applies only to single-segment published tables, such as bulk-loaded SF1 data. This is existing planner behavior, not changed here.

Acceptance:
- **Shared candidates:** `TestParallelStarSharesFlexCandidatesAcrossWorkers` checks a 2,000-candidate Flex fact on two workers. It needs exactly three Search calls, no directory range reads, each candidate read once, and drops the candidate whose row no longer matches. It fails before the change (serial, grant 0).
- **Integration:** `TestSQLFlexSnowflakeSharedCandidates` runs a Flex snowflake on Redis. Its plan must report `complete_flex_candidates`, and its rows must equal the eager join's, both on published tables and after an UPDATE and DELETE. It fails before the change (exact-directory ranges).
- **Demand limit:** `TestFlexFactKeyDemandUnionLimit` pushes a 1,500-key Flex demand and refuses it without Flex and for an unmarked OR.
- **Policy tests updated:**
  - The over-budget test sets the shared budget to the scan budget, so its single-count-then-ranges assertion still holds.
  - The admitted-selective test uses a set below two chunks, which stays serial.
- **SF1 Flex:** Q7 and Q8 fully fetched and validated, with rows read and times recorded.
