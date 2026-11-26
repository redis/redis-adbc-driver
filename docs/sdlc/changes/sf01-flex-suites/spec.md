# Spec

2026-11-26; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## What is measured

- **Suites:** TPC-H (22 queries), SSB (13) and TPC-DS (99), as exported unchanged by `benchmarks/olap-release/run.py` (DuckDB `tpch_queries()` / `tpcds_queries()`, `ssb-tools` `benchmark_queries/*.sql`).
- **Data at SF 0.1:**
  - TPC-H: DuckDB `dbgen(sf=0.1)`, 8 tables, 866,602 rows.
  - TPC-DS: DuckDB `dsdgen(sf=0.1)`, 24 tables, 1,176,170 rows.
  - SSB: `ssb-dbgen -s 0.1` for customer, part, supplier and lineorder, 626,353 rows in all. The date table is generated at its fixed spec size, 2,556 rows (`-s 1 -T d`), because `-s 0.1` also scales it, to 255 rows, which would leave most `lo_orderdate` values without a date row.
- **Driver:** a Linux ARM64 library built from main `a15b1e5c` with `go/build-linux.sh` (c-shared, `driverlib` tag, version linker flag).
- **Profile:** the SF1 Flex profile of [#648](https://github.com/redis/redis-adbc-driver/issues/648) and [flex-ssb-compacted](../flex-ssb-compacted/validation.md#deployment): Redis Software with a Redis 8.8.0 Flex database, 1 shard, 8 GiB RAM tier of 64 GiB, verified active 4x QPF, `adbc.redis.flex=true`, 60 s statement deadline, a 4-CPU / 8 GiB Linux client in the server's network namespace.
- **Harness protocol, unchanged:** per query, native DuckDB then direct Redis ADBC in fresh processes, each one warm-up and three fully fetched timed runs, then separate disk-backed multiset validation (relative 1e-7, absolute 1e-6). On Flex, the [compaction gate](../../../../benchmarks/olap-release/README.md#flex-index-compaction-gate) must pass before any query is timed and is rechecked before each query.

## Acceptance

- A suite passes when every query record has `duckdb_status` and `redis_status` `ok` from one build, with no client OOM kill.
- Each query record and the suite metadata are retained. The fixture's QPF and settings are verified through the management API before and after each suite.
- Not claimed: SF1 results, speedups, or comparisons with the RAM SF 0.1 TPC-DS record, which used another topology, client route and method.

## Known record labels

The harness writes `"scale_factor": 1` into `metadata.json` and reads `<suite>-sf1.duckdb` whatever the data's scale. Its `driver` field is the installed wheel's version (0.0.31), not the library loaded through `REDIS_ADBC_LIBRARY`. The [results README](../../../../benchmarks/olap-release/results/sf01-flex-a15b1e5c/README.md) records the actual scale, build and checksums. The harness files were not modified to change those labels.
