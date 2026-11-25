# Validation

2026-11-25; branch from main `682fae1f`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.** On SF1 Flex, Q19 is reliably under 60 s (4.5–10.0 s). Q1 now runs the partitioned aggregate on 32 workers but is not under 60 s: 238.9 s cold, 79.2 s with a warmer cache.

## Tests

- **Aggregate:** `TestSQLFlexPartitionedAggregate` uses a Flex table of 197,608 rows. Its Q1-shaped query has exact decimal and integer aggregates by group and Q1's `d <= DATE '1998-12-01' - INTERVAL '90' DAY` filter. It reports `partitioned_exact`, no server or compact tuples, at least 2 workers and exact results.
  - Copied onto main, it fails: the serial aggregate ran.
  - Copied onto the first WIP commit, which lacked the residual change, it fails: `distinctResidual` declined Q1's interval filter.
- **Eager join:** `TestSQLFlexEagerJoinParallelInputs` runs a Flex Q19-shaped eager join with NULL and missing keys. Its result matches the serial read, and the plan remains the eager join. The grant is 0 serially and at least 2 in parallel. Copied onto main, it fails (grant 0).

Results:
- `gofmt` clean; `go vet` reports nothing new.
- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...` passes (all 7 packages).
- On the shared Redis Software fixture (8.8.0, 1 shard, verified 4x QPF, TLS), `-run 'Partition|Aggregate|Join|Flex|Q19|Snowflake|Star|Candidate|Demand|Union|InList|Grouped|Decimal|Distinct' -skip 'LiveHash|Retained|Snapshot'` gave 260 passed, 42 skipped and 1 failed. The failure, `TestPartitionManagedCopyOwnership/application_index` ("it_partition_app_index is still indexing"), fails identically without these changes (see [#643's validation](../tpch-flex-read-path/validation.md#tests)).
- **Snapshot suites:** run with `scripts/ci_go_suites.py run --suite snapshot --shards 4` and the CI job's environment. The fixture was a dedicated Redis Software 8.8.0 database: 3 shards, API-verified active 4x QPF, mnp, 32 connections, noeviction.
  - Shard 0: 102 selected, 89 passed, 13 skipped.
  - Shard 1: 123 selected, 106 passed, 17 skipped.
  - Shard 2: 124 selected, 104 passed, 20 skipped.
  - Shard 3: 123 selected, 105 passed, 18 skipped.
  - No failures in any shard.

## Subset (10% TPC-H, local RAM fixture, Linux namespace route, 4 CPUs / 8 GiB)

Fully fetched and validated against DuckDB. In Flex mode:

| Query | Serial plan (first WIP build, same plans as main) | This change | Plan |
|---|---|---|---|
| Q1 | 5.86 s | 3.59 s | serial aggregate → `partitioned_exact`, 10 workers |
| Q19 | 4.80 s | 4.42 s | eager join; lineitem stays serial (no plain Flex index on these tables, and its filter is not broad) |
| Q3 / Q5 / Q6 / Q14 | — | 1.69 / 1.74 / 4.13 / 3.34 s | unchanged plans, validated |

RAM-mode Q1 validated in 4.04 s. The subset tables were created on the RAM profile, so they don't exercise Flex's shared candidates. The host was shared with other sessions' containers, so times are observations.

## SF1 Flex

**Setup.** The same deployment as [#643's validation](../tpch-flex-read-path/validation.md#sf1-flex):
- Redis Software 8.8.0 Flex, 1 shard, 8 GiB RAM tier, verified 4x QPF.
- One 4-CPU / 8 GiB Linux client through `--network container:redis-adbc-flex`.
- Exclusive and read-only, 2026-11-25 03:12–03:18Z.

Library `v0.0.31-flexwip.q119b`, built from this branch, with the 32-worker Flex default. Each run was a fresh process, fully fetched and validated against the SF1 DuckDB source.

| Query | Run | Seconds | Rows read | Workers | Plan |
|---|---|---|---|---|---|
| Q19 | #636, main `14fcd545` | 804 | 6,008,300 | 0 | serial eager join |
| Q19 | single run | 9.99 | 221,462 | 32 | eager join; lineitem via 214,377 shared candidates |
| Q19 | warm-up + 3 timed (60 s deadline) | 7.13, then **4.56 / 5.53 / 4.54** | — | 32 | same |
| Q1 | #636, main `14fcd545` | 95.8 | 6,001,215 | 0 | serial aggregate |
| Q1 | first run | 238.9 | 6,001,215 | 32 | `partitioned_exact` |
| Q1 | second run | 79.2 | 6,001,215 | 32 | `partitioned_exact` |

Q1 keeps 98.6% of lineitem, so it reads every row.
- **Cold:** the first run followed Q19, which read only 221k lineitem rows, so most of lineitem was cold. Its workers spent about 97% of their time waiting on replies (7,160 worker-seconds), and it allocated 63 GB.
- **Warmer:** the second run waited about 90% of the time (2,290 worker-seconds).
- **#636's serial 95.8 s** followed a full lineitem scan (Q19's), so its cache state isn't comparable to the cold run.
- **Q1 is not under 60 s on this deployment.** Its full read is bound by the single shard's flash reads when cold. When warm, roughly 76k rows/s, it allocates about 10 KB per row on a 4-CPU client.

## Not established

- **Q4 and Q18**, also in #636, were not rerun.
- **A Q1 warm-cache series:** only two runs, in different cache states.
- **Human review** is pending.
