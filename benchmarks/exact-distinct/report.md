# Exact COUNT DISTINCT: issue #213

Measured October 7, 2026, against main commit
`6d7d87694cc523741768c029db97b6af5dba52e2`, including #211's narrow exact scans
and #212's incremental aggregation. The modified library adds this issue's
collision-checked tuple strategy. No original Cloud experiment was changed.
These are local comparisons, not patched Cloud or full-scale ClickBench
results.

## Exactness and scope

Eligible queries contain only COUNT DISTINCT aggregates over plain indexed
columns, with optional grouping on such columns. Supported kinds are strings,
16/32/64-bit integers, booleans, dates, times and timestamps. Floating point,
decimals, intervals, adopted/in-memory tables, virtual defaults, expressions,
unindexed columns and mixed aggregates retain the exact row accumulator path.
`aggregate_pushdown=none` disables the strategy; `exact` and `all` use the
same exact checks.

For safe sorting vectors, GROUPBY needs no HASH loading. Otherwise `LOAD *`
**before** GROUPBY supplies canonical HASH strings, preserving BIGINTs above
2^53 and embedded NUL bytes. Named loads, including aliases, still read the
sorting vector. The modified driver adds **zero stored fields and zero index
attributes**; table ingestion, metadata and index storage are unchanged.
The cost is raw HASH loading on the server and temporary per-tuple state.
Redis's [FT.AGGREGATE documentation](https://redis.io/docs/latest/commands/ft.aggregate/)
explains why loading incurs additional server work.

Raw values must round-trip through typed decoding/encoding with identical
bytes. Alternate spellings, such as `+9007199254740992`, make the whole query
fall back instead of counting SQL-equal values separately. Complete tuple
keys include all grouping columns and all arguments; any NULL argument
excludes that tuple from COUNT. Multiple distinct calls use separate
pipelines, and equivalent calls share one pipeline. Pure comparisons over
tuple columns recheck residual WHERE/FILTER predicates after deduplication.
Other residual expressions fall back. A separate group universe preserves
zero-count groups when every aggregate is filtered. Multiple pipelines do
not provide a snapshot across concurrent writes, consistent with the
existing driver's concurrency guarantees.

**GROUPBY alone is insufficient proof of equality.** The tested Search
[GROUPBY implementation](https://github.com/RediSearch/RediSearch/blob/v8.6.0/src/aggregate/group_by.c)
identifies groups by a 64-bit hash. For every tuple column this strategy adds
TOLIST plus MIN/MAX of an existence flag. [TOLIST](https://github.com/RediSearch/RediSearch/blob/v8.6.0/src/aggregate/reducers/to_list.c)
uses a dictionary that compares full values on hash collisions. Its
[string comparison](https://github.com/RediSearch/RediSearch/blob/v8.6.0/src/value/value.c)
uses lengths and memcmp, including bytes after a NUL. At most one distinct
value and uniform NULL presence in every column prove that a returned hash
group contains exactly one tuple. A failed check discards all partial counts
and invokes the exact row path. No probabilistic COUNT_DISTINCT reducer is
used and no per-shard distinct counts are added.

For NUL-containing columns, MODULE LIST must confirm Search 8.6 or newer on
all known OSS cluster primaries; older releases or denied inspection use the
row fallback. Earlier Search [string comparison](https://github.com/RediSearch/RediSearch/blob/v8.4.0/src/value.c)
used strncmp and cannot establish binary-safe TOLIST equality. The new cursor
parser records cardinalities only for explicitly requested array aliases;
other aggregate reply parsing retains its existing behavior.

## Method

macOS arm64 (Apple M4), Go 1.26.6, Docker with 10 virtual CPUs, Redis 8.6.2
with Search module version 80600. A dedicated standalone loopback Redis
container held the benchmarks, separate from integration-test containers.
Other local work shared the host, so timings are variable.

The official sample is the first 100,000 rows of the public
`hits_compatible/hits.parquet`, all 105 columns and default indexes, copied
with user approval for #211. SQL is ClickBench's PostgreSQL variant at
`29b09614bf8ffcbd46ac83f64804cb060422c15d`. Q5 counts distinct UserID;
Q6 distinct SearchPhrase; Q9 distinct UserID per RegionID; Q14 distinct
UserID per nonempty SearchPhrase.

Each query/strategy has three trials in fresh processes and RESP2 ADBC
connections; strategy order alternates between trial sets. Timers include
execute and Arrow fetch, excluding connection setup and independent reference
validation. Hot latency is min(trials 2, 3). All other table metrics are
medians of the three trials. A loopback TCP proxy counts actual Redis request
and reply bytes during execution/fetch, including query-time metadata,
cursor and HMGET replies. It excludes connection setup and the server-memory
sampler's separate connection. Process peak RSS is ru_maxrss captured after
Arrow fetch and before reading reference data; it includes Python, Arrow,
Go and native runtime memory.

The independent references use Python integer/set equality from source
columns and exclude SQL NULLs. Grouped top-10 checks validate unique keys,
exact counts, descending order and the cutoff, allowing tied groups. **All
72 end-to-end trials passed.** [query-results.json](query-results.json) stores
every measurement and returned row. No fixture or credentials are checked in.

## Official 100K-row ClickBench sample

Each paired cell is **original / modified**.

| Query | Hot latency | Redis reply bytes | Driver process peak RSS |
|---|---:|---:|---:|
| Q5 | 1.869 / 1.176 s | 10.470 / 0.818 MiB | 122.7 / 102.2 MiB |
| Q6 | 0.364 / 0.051 s | 9.700 / 0.712 MiB | 125.2 / 97.3 MiB |
| Q9 | 1.921 / 1.159 s | 12.609 / 1.393 MiB | 124.0 / 106.3 MiB |
| Q14 | 1.822 / 1.234 s | 13.506 / 2.498 MiB | 125.5 / 114.9 MiB |

Reply traffic fell 81–93%, hot latency 32–86%, and cumulative Go allocation
90–98% on these four sampled queries. These ranges describe this fixture,
not a universal improvement.

| Query | SQL-matching rows | SQL groups | Tuples after SQL filtering | Cursor records | HMGET row replies | Redis request bytes |
|---|---:|---:|---:|---:|---:|---:|
| Q5 | 100,000 | 1 | 7,248 | 100,000 / 7,248 | 100,000 / 0 | 6.762 / 0.001 MiB |
| Q6 | 100,000 | 1 | 3,644 | 100,000 / 3,644 | 0 / 0 | 0.001 / 0.000 MiB |
| Q9 | 100,000 | 403 | 7,255 | 100,000 / 7,255 | 100,000 / 0 | 6.762 / 0.001 MiB |
| Q14 | 13,005 | 3,643 | 4,929 | 100,000 / 10,421 | 100,000 / 0 | 6.762 / 0.001 MiB |

Logical cursor/HMGET counts come from separate Go profile workers. They count
actual reply records without reparsing row maps. Q14's exact string predicate
needs a residual recheck: its index candidate stream includes all 100K rows,
so the server returns 10,421 tuples before the driver discards empty phrases;
4,929 tuples satisfy the SQL predicate. This is why matching rows, distinct
tuples and transferred records are different measures.

## Driver allocations

Separate fresh Go workers execute the same plans without Arrow conversion.
GC runs before each query. MemStats deltas capture cumulative allocated bytes
and allocation counts; HeapAlloc is sampled every 20 ms, including the final
reading. Sampled peaks may miss short peaks and are not RSS or precise live
object sizes. The result remains alive for a post-query GC retained-heap
reading. Profile hooks count records without decoding replies a second time.
All 72 raw profiles, including both synthetic cases, are in
[heap-results.json](heap-results.json).

| Query | Total allocated | Allocation count | Sampled peak Go heap |
|---|---:|---:|---:|
| Q5 | 284.2 / 9.0 MiB | 3,302,505 / 195,809 | 31.9 / 8.6 MiB |
| Q6 | 217.3 / 4.7 MiB | 2,140,426 / 94,848 | 28.3 / 5.9 MiB |
| Q9 | 294.9 / 13.7 MiB | 3,968,792 / 360,308 | 30.7 / 14.5 MiB |
| Q14 | 295.7 / 29.7 MiB | 3,357,589 / 516,696 | 25.5 / 19.2 MiB |

The old path creates exact client distinct sets while decoding the source
records. The modified path has no client distinct sets: it keeps only a
count/result/representative per SQL group while visiting deduplicated cursor
pages. Exact full-value TOLIST sets and tuple groups live on Redis instead.

## Low/high-cardinality fixtures and server cost

Both synthetic fixtures contain 100K rows and three columns. RegionID is
`i % 17`. Low cardinality uses UserID `2^53 + i % 31` and SearchPhrase
`phrase(i % 23)`; high cardinality uses UserID `2^53 + i` and SearchPhrase
`phrase(i)`. All are non-NULL; all phrases are nonempty. They deliberately
cover repeated values and one distinct value/tuple per input row.

| Fixture | Query | Modified cursor records | Hot latency | Redis reply bytes | Driver process peak RSS |
|---|---|---:|---:|---:|---:|
| low | Q5 | 31 | 1.603 / 0.059 s | 10.173 / 0.004 MiB | 124.1 / 90.6 MiB |
| low | Q6 | 23 | 0.309 / 0.009 s | 9.665 / 0.003 MiB | 124.1 / 90.7 MiB |
| low | Q9 | 527 | 0.311 / 0.093 s | 15.141 / 0.098 MiB | 124.6 / 93.5 MiB |
| low | Q14 | 713 | 0.326 / 0.072 s | 15.141 / 0.144 MiB | 125.5 / 93.5 MiB |
| high | Q5 | 100,000 | 1.892 / 0.351 s | 10.269 / 10.777 MiB | 126.7 / 111.9 MiB |
| high | Q6 | 100,000 | 0.254 / 0.285 s | 10.172 / 10.468 MiB | 132.2 / 111.8 MiB |
| high | Q9 | 100,000 | 0.277 / 0.419 s | 15.647 / 18.485 MiB | 135.6 / 119.8 MiB |
| high | Q14 | 100,000 | 0.359 / 0.501 s | 15.647 / 20.958 MiB | 438.0 / 434.6 MiB |

**High cardinality regresses:** Q6/Q9/Q14 are slower here, verification
payloads increase reply traffic, and Q14 barely changes process RSS. Raw
loading and collision verification have real costs. At full cardinality no
source tuples are eliminated. A future cardinality/cost-based strategy and
spill/resource limits are needed before claiming arbitrary-scale efficiency.

Redis INFO memory is sampled through a separate connection every 10 ms. The
following is max(sampled used_memory) minus the reading before each query,
then the median of three trials. These are **sampled extra-memory lower
bounds, not true peak RSS**. Short peaks can be missed; INFO may wait behind
server execution, especially for queries finishing in one cursor page. A
zero sample does not mean zero query memory. The sampler includes transient
query/output/client buffers; it does not isolate just the tuple reducers.

| Fixture | Query | Sampled extra Redis used_memory |
|---|---|---:|
| clickbench | Q5 | 1.18 / 1.25 MiB |
| clickbench | Q6 | 1.38 / 1.13 MiB |
| clickbench | Q9 | 1.27 / 2.59 MiB |
| clickbench | Q14 | 1.38 / 10.00 MiB |
| low | Q5 | 1.19 / 0.00 MiB |
| low | Q6 | 1.47 / 0.00 MiB |
| low | Q9 | 2.09 / 0.00 MiB |
| low | Q14 | 2.08 / 0.05 MiB |
| high | Q5 | 1.19 / 33.21 MiB |
| high | Q6 | 1.64 / 27.35 MiB |
| high | Q9 | 2.10 / 57.78 MiB |
| high | Q14 | 2.10 / 58.32 MiB |

The high-cardinality cases move tens of MiB of additional state to the server.
There is no new storage/index overhead, but there is no spill or new query
memory limit either. Existing server resource limits and errors are honored.

## Validation

- Final complete Go suite passed against standalone Redis 8.6.2.
- Targeted final suites passed with RESP3 and RESP2 on three OSS shards.
- Fixtures repeat every tuple under different keys/hash slots, verifying
  overlapping values across shards and multiple cursor pages.
- Regressions cover BIGINT endpoints and values above 2^53, leading/middle/
  trailing NUL strings, normalization/empty strings, missing values, NULL
  groups, multi-column DISTINCT, FILTER zero-count groups, residual predicates,
  HAVING/ordering/LIMIT, renamed columns, virtual defaults and unindexed
  fallback. Pure residual results are compared with the row accumulator path.
- Forced merges emulate hash collisions for unequal integers, NUL strings and
  NULL/value mixtures. They are rejected by full-value verification.
- Cancellation after cursor creation leaves zero open index cursors.
- Older Search versions and denied MODULE LIST preserve exact NUL-string
  fallback results.
- Original and final native libraries built; formatting, Python syntax and
  staged whitespace checks passed.

## Reproduce

Keep the public Parquet, PostgreSQL create.sql and queries.sql outside the
repository. Use a dedicated standalone non-TLS loopback Redis server, set
REDIS_URI with `?protocol=2`, and build separate original/modified checkouts.
Examples use macOS `.dylib`; Linux uses `.so`.

```sh
make -C go build
uv run --no-project --with pyarrow --with adbc-driver-manager --with redis \
  python benchmarks/exact-distinct/run.py --phase load \
  --driver go/build/libadbc_driver_redis.dylib \
  --parquet /path/hits-100k.parquet --ddl /path/create.sql \
  --queries /path/queries.sql

uv run --no-project --with pyarrow --with adbc-driver-manager --with redis \
  python benchmarks/exact-distinct/run.py --phase query \
  --driver go/build/libadbc_driver_redis.dylib \
  --baseline /path/original/go/build/libadbc_driver_redis.dylib \
  --parquet /path/hits-100k.parquet --ddl /path/create.sql \
  --queries /path/queries.sql --output /tmp/query-results.json

# The original checkout needs the same opt-in profiler for the comparison.
cp go/exact_distinct_profile_test.go /path/original/go/
go -C go test -c -o /tmp/modified.test
go -C /path/original/go test -c -o /tmp/original.test
python benchmarks/exact-distinct/profile.py \
  --baseline /tmp/original.test --worker /tmp/modified.test \
  --queries /path/queries.sql --output /tmp/heap-results.json
```

Repeat load/query with `--synthetic low --table it_213_low` and then
`--synthetic high --table it_213_high`; pass each table to profile.py.
The table names must begin `it_213_`. Load refuses to replace an existing
table. Use `--phase cleanup` with the same table/fixture arguments to drop
each dedicated fixture. The profiler requires preloaded data and never
creates or drops tables.
