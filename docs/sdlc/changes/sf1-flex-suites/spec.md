# Spec

2026-11-26; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## What is measured

- **Suites:** TPC-H (22 queries), SSB (13) and TPC-DS (99), exported unchanged by `benchmarks/olap-release/run.py`.
- **Data at SF1:** the retained private sources of the earlier SF1 records, with the same SHA-256s (TPC-H `d652d81d…`, SSB `5e029600…`, TPC-DS `508d4f5a…`). TPC-H 8,661,245 rows, SSB 6,235,727, TPC-DS 19,557,375.
- **Driver:** a Linux ARM64 library built from main `7c83c486` with `go/build-linux.sh` (c-shared, `driverlib` tag, version linker flag). Its Go sources are those of `a15b1e5c`.
- **Profile:** the SF1 Flex profile of [#648](https://github.com/redis/redis-adbc-driver/issues/648): Redis Software with a Redis 8.8.0 Flex database, 1 shard, 8 GiB RAM tier of 64 GiB, verified active 4x QPF, `adbc.redis.flex=true`, 60 s statement deadline, a 4-CPU / 8 GiB Linux client in the server's network namespace, shared client cgroup.
- **One fixture per suite:** each suite gets a newly created database, is loaded, gated and timed, and the fixture is removed before the next suite starts.
- **Harness protocol, unchanged:** native DuckDB then direct Redis ADBC in fresh processes, each one warm-up and three fully fetched timed runs, then separate validation. The [Flex compaction gate](../../../../benchmarks/olap-release/README.md#flex-index-compaction-gate) must pass before any query is timed, with the opt-in exemption for indexes of at most 10,000 documents.

## Acceptance

- Every query of every suite has a record from one build, either validated or failed with its phase and cause.
- Each failing query gets its own issue, with the observation, the profile and the record path.
- Not claimed: speedups, controlled comparisons with the earlier records (other fixtures and cache states), or behavior on more shards, a larger RAM tier or Redis Cloud.
