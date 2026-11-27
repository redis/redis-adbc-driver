# Validation

2026-11-27 (UTC). Base: main `3fada6f5`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **SF1 on Software Flex:**
  - **Q40:** timed out before; now 0.91 s.
  - **Q80:** OOM-killed before; now 7.93 s.
  - **Q75 and Q78** now stream, but still time out.
- **All 99 TPC-DS queries** validate at SF 0.1.
- **Same-fixture A/B against main at SF 0.1:** Q40, Q75, Q78 and Q80 are 1.8–8.5× faster; unaffected queries are unchanged.

## Unit tests (no Redis)

`join_outer_stream_test.go`, on the in-memory fixture of the snowflake tests. There, lineitems 121–125 and those with NULL order keys have no order, order key 5 has two orders, and partsupp has only every third supplier.

- **`TestSnowflakeOuterLeafMatchesEagerJoin`:** five shapes, each with exactly one outer dimension, run as `streaming_snowflake_join` and return the eager join's rows:
  - a leaf with multi-match keys;
  - an anti-join (`o_orderkey IS NULL`);
  - aggregates over the outer side: `SUM(COALESCE(…))`, `SUM` and `COUNT` of an outer column, and `COUNT(*)`;
  - an outer-side `ON` filter;
  - a composite key.
- **`TestSnowflakeOuterLeafRejectsUnsafeShapes`:** four shapes stay eager:
  - a preserved-side `ON` filter;
  - an outer item with a child;
  - a non-equi `ON`;
  - a NULL-supplying table that would be the fact.
- **`TestStarRowProbeOuterDimension`:** the parallel probe, through both `row` and `ownedRow`, emits matched rows twice for a two-row match, NULL-extends an unmatched row and a NULL key, and drops an inner miss.
- **Before the change,** with only the `outer` field added, the five equivalence cases and the probe's unmatched and NULL-key cases failed. The rejection cases passed.
- **Existing tests:** all `TestSnowflake*` tests, including the spill tests, pass.
- **Unit-only suite:** `cd go && env -u REDIS_URI -u REDIS_FLEX_URI go test -count=1 -p 1 ./...` passes; exit 0.
- **Formatting and vet:** `gofmt -l` reports nothing. `go vet` reports only an existing warning in `live_ingest_creation_integration_test.go`.

## Deployment

The fixture used for [#729](https://github.com/redis/redis-adbc-driver/pull/729):
- Redis 8.8.0 Flex, 1 shard, 8 GiB RAM tier of 64 GiB, verified active 4x QPF, TLS, AOF every second, Search disk settings 8/2/3 MiB.
- TPC-DS SF1 in schema `tpcds` and SF 0.1 in `tpcds01`, managed tables, gated for compaction.
- A 4-CPU / 8 GiB Linux client in the server's network namespace.
- Other sessions' Redis containers shared the Docker host.

**Libraries:**
- main: `717b20d9…`; its Go sources are those of `3fada6f5`.
- main plus the #696 fix: `727b51e2…`, for the SF1 "before" diagnostics.
- This change: `185d8eb2…`.

## TPC-DS SF1 on Flex

Harness focused runs, each validated against DuckDB:

| Query | Before (main planning) | This change |
|---|---|---|
| Q40 | timeout in the warm-up (63.2 s), 1.19M rows processed | **0.91 s** median (warm-up 10.36 s; 1.41 / 0.91 / 0.78 s); parallel streamed join, 146,970 rows |
| Q80 | OOM-killed 40.4 s into the warm-up, 7.4 GiB heap | **7.93 s** median (warm-up 18.94 s; 9.86 / 7.93 / 6.23 s); 525,309 rows, 1.33 GiB allocated |
| Q75 | timeout (60.9 s), eager `lookup_join_order`, 485,061 rows | timeout (the query timed out), parallel streamed join, 1,176,682 rows in 60 s |
| Q78 | timeout (66.2 s), 3.16M rows, 6.8 GiB heap | timeout, parallel streamed join, 2,310,259 rows in 60 s |
| Q72 (two `LEFT JOIN`s, not admitted) | timeout, `lookup_join_order` | timeout, `lookup_join_order` |

**Why Q75 and Q78 still time out:** they read most of all three sales tables. Q78's CTEs aggregate every year, and `ss_sold_year = 2000` is applied afterwards. Q75 reads every Books sale of all years in three channels.

**Q72's first SF1 diagnostic** never reached Redis: its DuckDB reference failed the harness's artifact check. Both runs here completed the reference.

## TPC-DS SF 0.1 on Flex

`supervise.py tpcds --reuse-loaded`: **99 of 99 validated**.

Against the previous pass on this fixture (the reduce-outer-joins branch), the plan kinds changed only for:
- Q40 (now `streaming_snowflake_join`);
- Q78 (now `streaming_snowflake_join`);
- Q93, which that branch's rule reduces and this one does not.

**Same-fixture A/B,** harness focused runs alternating the libraries:

| Query | main | change | main | change |
|---|---:|---:|---:|---:|
| Q40 | 1.11 s | **0.13 s** | 1.10 s | **0.12 s** |
| Q78 | 12.28 s | — | 10.35 s | **5.52 s** |
| Q75 | 3.67 s | **2.10 s** | 3.29 s | **1.79 s** |
| Q80 | 6.28 s | **0.77 s** | 4.16 s | **0.68 s** |
| Q72 | 4.12 s | 3.57 s | 2.97 s | 3.09 s |
| Q5 | 0.46 s | 0.30 s | 0.29 s | 0.29 s |
| Q77 | 0.89 s | 0.64 s | 0.67 s | 0.64 s |

"—" marks a record with no Redis run: its native DuckDB reference failed the harness's artifact read-back check (`validator scratch sort-0`), as Q72's did in the SF1 diagnostic run. That is a harness problem, not this change.

## Not covered

- **Q75 and Q78 at SF1** (#710, #713): moving outer filters into the CTEs, and the flash read rate.
- **Outer joins between dimensions, and `RIGHT`/`FULL` joins.**
- **The integration suite and the PostgreSQL differential corpus** were not run locally; see the PR's CI.
- **Review:** human review is pending.
