# Plan

2026-11-25; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Extract the sort spill's field codec into `spill_rows.go` (`spillRowCodec`).
2. Add `join_spill.go` (partition files, `joinSpilledFacts`) and the spilling `keep`, the
   partition-cap fallback and the serial re-read in `streamJoinPages`.
3. Unit tests over the in-memory snowflake fixture: a cap/partition sweep against the eager join
   and the fallbacks.
4. Integration: add `cap_fallback` (failing on main) and `spilled` to
   `TestSQLSnowflakeJoinsManaged`; fix the parallel reader's cap stop.
5. Measure subset Q9 (default cap, lowered cap with spill, lowered cap without spill) for peak
   live heap and identical results.
6. Run the unit-only pass and the join / snowflake / star / sort integration selections.
7. Open a PR that completes #654.
