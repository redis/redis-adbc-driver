# Plan

Date: 2026-11-25. Baseline: `c8f6f50d`. Issue: [#627](https://github.com/redis/redis-adbc-driver/issues/627).
See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Measure Q4 at SF 0.1 on RAM and on 1-shard Flex through native DuckDB, direct Redis ADBC and DuckDB over ADBC, with DuckDB JSON profiles.
2. Probe Flex 8.2.6-58 Search capabilities and visibility-count correctness on flash-backed data.
3. Split `snapshotSearchable` from `snapshotIndexable` (`go/snapshot_candidates.go`). Add `snapshotCountQueries` and `snapshotVisibilityQueries` (`go/snapshot_visibility.go`). Use the count path in `go/statistics.go`.
4. Add `TestSnapshotFlexApproximateStatisticsSearchCounts` (`go/snapshot_flex_statistics_integration_test.go`), and confirm it fails on the baseline.
5. A/B Q4 on Flex against `main`, then run the unit suite, the Flex snapshot and Flex tests, and the full RAM suite.
6. Update `docs/adbc-statistics.md` and `docs/redis-flex-stopgap.md`.
