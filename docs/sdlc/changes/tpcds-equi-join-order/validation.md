# Validation and review

Date: 2026-11-25. Baseline: `064eb12d` (query runs used `0ebee353`, which has the same driver code).
Issue: [#649](https://github.com/redis/redis-adbc-driver/issues/649). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Environment

Local Docker Desktop on Apple silicon. Redis Software (Redis 8.8.0) RAM database, 3 shards, authenticated TLS. 4× QPF was verified by `software-setup.sh`: active, scaling factor 4, 6 Search workers, `mnp` routing, 32 connections. Database memory was raised from 1 GiB to 4 GiB for the TPC-DS data and then restored for the Go suite.

The client was macOS: the driver built with `make -C go build`, Python 3.14 with `adbc-driver-manager` 1.13.0, and DuckDB 2.0.1. It used Docker's published port, not the server's network namespace.

This is not the recorded SF1 Flex benchmark profile.

## TPC-DS at SF 0.1

DuckDB's `dsdgen(sf=0.1)` generated 24 tables, 1,176,170 rows. They were loaded through the driver with `adbc_ingest`, and every table's row count was checked. All 99 queries are the unmodified `tpcds_queries()` SQL. Each ran once, cold, under a 60 s statement deadline, with a full fetch. Results were compared as row multisets against DuckDB, with the harness tolerance (relative 1e-7, absolute 1e-6). There was no warm-up and no repeats, so these are not benchmark medians.

| Build | Validated | Timed out | Mismatch / other error |
|---|---:|---:|---:|
| Baseline | 94 | 5 (Q15, Q19, Q35, Q45, Q72) | 0 |
| This change | 97 | 2 (Q35, Q72) | 0 |

- Q15: 62.3 s timeout → 0.24 s. With a 900 s deadline, the baseline finished correctly in 332 s.
- Q19: 74.0 s timeout → 0.17 s.
- Q45: 60.0 s timeout → 0.26 s.
- Also faster: Q46 25.6 s → 1.9 s, Q68 2.4 s → 0.57 s.
- No query changed status for the worse, and none slowed by more than max(1 s, 25%).
- Summed time of queries that validated in both runs: 232.4 s → 203.5 s.

Baseline execution diagnostics (`adbc.redis.execution_diagnostics_file`):

| Query | Plan | Redis commands | Redis reads | Driver allocation |
|---|---|---:|---:|---:|
| Q15 | `lookup_join_order` | 9,254 | 2 MB | 104 GB in 120 s |
| Q19 | `lookup_join_order` | 15,231 | 3 MB | 45 GB |
| Q45 | `select_scan` | 8,366 | 2 MB | 89 GB |

A CPU profile of Q15 put the time in `executor.joinRows` building merged rows and evaluating `SUBSTRING`, with about half the CPU in garbage collection.

The remaining failures have other causes:
- Q72: 85 GB allocated in written join order; it has LEFT JOINs.
- Q35: 811 MB read and 106 Redis worker-seconds in a streaming snowflake join; `FT.AGGREGATE` hit the deadline.

## Tests

- `TestJoinOrderPrefersEquiJoinOverCrossTableResidual` (OR and `<>` subtests) failed on the baseline with order `[3 0 2 1]`, and passes with the change. `TestJoinOrderStillUsesResidualWhenNoKeyConnects` passes on both.
- `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages OK.
- Full Go suite on the fixture above: see the PR.

## Limits

SF 0.1 in RAM mode only. No SF1, Flex or Cluster API run was done. The two remaining timeouts need separate work. Human review is pending.
