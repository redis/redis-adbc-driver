# Intent

Date: 2026-11-25. Source baseline: `c8f6f50d`.
Status: implemented on a branch; awaiting human review, not merged.
Requested by the user: work on #627 at SF 0.1, and retest whether the newest Flex release supports snapshot indexing. Rebased onto `844d8123` (#689).
Issue: [#627](https://github.com/redis/redis-adbc-driver/issues/627). See [spec](spec.md), [plan](plan.md), [validation](validation.md).

#627 reports that TPC-DS Q4 through DuckDB over a Redis ADBC catalog timed out on Software Flex. At SF 0.1 on a 1-shard Flex database, Q4 took 44 s through that path, of which 33.6 s was DuckDB binding. That binding time came from `GetStatistics`:

- DuckDB's ADBC scanner requests approximate statistics for every table reference while binding. Q4 has nine references, and the scanner caches nothing across them.
- On Flex, `snapshotIndexable` is false, which also turned off the per-segment visibility counts that approximate statistics use. So each request read every row ID in the snapshot directories. `store_sales` took 5.6 s on Flex, against 0.19 s on RAM.

Desired: approximate row counts on Flex use the Search visibility counts that RAM databases already use, with the same values.

Excluded:
- Scans, aggregates and other readers on Flex, which keep the exact directories.
- Parallel scanner readers, and caching inside the scanner.
- Flex read throughput, which is server-bound on one shard.
