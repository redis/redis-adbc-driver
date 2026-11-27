# Plan

2026-11-26 (base main `3fada6f5`); see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Read the streamed join's admission, dimension build and the three probe paths (serial, parallel `starRowProbe`, spilled), and fact key demand.
2. Add `join_outer_stream_test.go`:
   - five outer-leaf shapes on the in-memory fixture, each compared with the eager join: multi-match keys, anti-join, aggregates over the outer side, an outer-side `ON` filter, a composite key;
   - four shapes that must stay eager;
   - a direct `starRowProbe` test of matched, unmatched, NULL-key and inner-miss rows.

   Run it with only the `outer` field added, and record the failures.
3. Implement admission in `planSnowflakeShape` and NULL-extension in both probes. Make fact key demand skip outer dimensions, and turn spilling off for outer shapes.
4. Run the snowflake and spill tests and the unit-only suite.
5. On the TPC-DS SF1 Software Flex fixture, run Q40, Q75, Q78 and Q80 alone, and compare with the main diagnostic run.
6. Run all 99 TPC-DS queries at SF 0.1 on the same fixture, then an A/B against main for the queries whose plans changed.
