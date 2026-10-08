# Incremental driver-side aggregation: issue #212

Measured October 7, 2026, on the first 10,000 and 100,000 official ClickBench
rows, all 105 columns with their default indexes. The source is the same
`hits_compatible/hits.parquet` fixture copied with user approval for #211.
The PostgreSQL SQL variant came from ClickBench commit
`29b09614bf8ffcbd46ac83f64804cb060422c15d`.

## Change

`driverAggregate` now updates group accumulators from each cursor page
before requesting the next one. It no longer collects every decoded input
row first. Direct key lookups are visited in batches of at most 1,000 rows.
Cursor cleanup runs on success, cancellation and reduction errors.

Only fields used after reduction remain in a group's representative row:
projection, HAVING, ordering, windows, correlated-subquery dependencies and
ordered-set direct arguments. Fresh Redis row maps are pruned after their
aggregate inputs have been consumed. Shared in-memory rows are copied to
preserve their contents for other scans. Completed groups release their
accumulators and ordered-set inputs during result finalization.

Eligible ordered aggregates with LIMIT/OFFSET retain at most LIMIT + OFFSET
projected rows in a worst-first heap, then sort that bounded set. Every group
still evaluates its projection and sort expressions; errors and expression
order are preserved. Original group ordinals preserve stable ties, including
NULL placement and multiple sort keys. Windows, DISTINCT, unbounded limits
and limits covering all groups retain the existing complete ordering path.

The implementation is based on `53c7bbf0b04404445674fa2a947960b49474feeb` and
is independent of draft #216. Both libraries use that base, with only this
patch in the modified driver. **#211's narrow loading optimization is not
included**: exact BIGINT scans still transfer `LOAD *` rows here.

## End-to-end latency and peak RSS

Measurements used macOS arm64, Go 1.26.6 and Redis 8.6.2 in an isolated
standalone Docker container through localhost. Docker has 10 virtual CPUs.
Other local services and integration tests on a separate Redis container
shared the host; timing varies with host load. These are local comparisons,
not Redis Cloud or full-scale ClickBench results.

Three trials per strategy/query used fresh processes and RESP2 ADBC
connections, alternating strategy order between trial sets. Timers include
execution and Arrow fetch, excluding connection setup and validation. Hot
latency is the smaller of trials 2 and 3. Peak RSS is the median of the
three fresh-process `ru_maxrss` measurements, captured immediately after
Arrow fetch and before reading reference data. RSS includes Python, native
library/runtime and query memory; it is not just Go's heap. Neither metric
claims a cold run.

| Input rows | Query | Groups | Original hot latency | Incremental hot latency | Original peak RSS | Incremental peak RSS |
|---:|---|---:|---:|---:|---:|---:|
| 10,000 | Q9 | 128 | 0.405 s | 0.405 s | 145.7 MiB | 136.2 MiB |
| 10,000 | Q17 | 1,016 | 0.406 s | 0.401 s | 144.9 MiB | 136.5 MiB |
| 10,000 | Q33 | 10,000 | 0.408 s | 0.418 s | 146.6 MiB | 166.6 MiB |
| 100,000 | Q9 | 403 | 3.856 s | 3.924 s | 348.2 MiB | 141.1 MiB |
| 100,000 | Q17 | 10,421 | 3.896 s | 3.841 s | 349.4 MiB | 164.7 MiB |
| 100,000 | Q33 | 100,000 | 4.080 s | 4.010 s | 692.3 MiB | 622.7 MiB |

Q9 counts distinct UserIDs per RegionID. Q17 groups UserID/SearchPhrase.
Q33 groups WatchID/ClientIP and computes COUNT, SUM and AVG; every input row
is its own group in these samples. The lower-cardinality queries benefit
substantially in peak memory at 100K rows. **High-cardinality group state
remains expensive, and the measurements do not establish a universal latency
or RSS improvement.** The tables and raw trials show the regressions and
variation as well as the gains.

All 36 trials passed independent references from source columns using Python
integer arithmetic, sets and grouped counts. Q33 verifies exact SUM and
COUNT and AVG to relative tolerance 1e-12. Top-10 checks verify ordering,
unique group keys, exact group values and the cutoff, allowing tied groups
at the cutoff. [query-results.json](query-results.json) contains every trial
and returned row.

## Go allocation and heap profiles

Separate Go test workers execute the same planned queries without Arrow
conversion. Each query/strategy/trial has a fresh process. Profiles record
cumulative allocated bytes and allocation counts using `runtime.MemStats`.
A sampler reads HeapAlloc every 20 ms and includes the final measurement;
the sampled peak can miss shorter peaks. It is not a precise live-object
measurement or RSS. GC runs before each query. A second GC after execution
measures retained result heap while keeping the result alive. Raw profiles
for both input scales are in [heap-results.json](heap-results.json).

The following are medians of three profiles at 100K input rows:

| Query | Original sampled peak heap | Incremental sampled peak heap | Original total allocated | Incremental total allocated |
|---|---:|---:|---:|---:|
| Q9 | 247.8 MiB | 50.1 MiB | 1984.8 MiB | 1972.5 MiB |
| Q17 | 259.8 MiB | 76.4 MiB | 2015.6 MiB | 2002.9 MiB |
| Q33 | 576.2 MiB | 509.1 MiB | 2460.8 MiB | 2444.0 MiB |

Cumulative allocation remains dominated by parsing and decoding the wide
Redis replies; page consumption primarily reduces simultaneous retention.
Distinct sets and one accumulator set per group remain necessary. Bounded
output selection does not bound the number of groups or distinct entries.
Deleting unused representative fields releases their values; Go maps do not
necessarily shrink their allocated capacity.

## Validation and limits

The final complete Go suite passed against standalone Redis. Targeted
regressions passed on standalone RESP2 and a three-shard Redis cluster.
Coverage includes exact SUM/AVG above 2^53, DISTINCT, NULL and missing fields,
empty input, interval/NaN group equality, multiple cursor pages, direct key
lookups, grouped subqueries, ordered-set direct arguments, windows, HAVING,
LIMIT/OFFSET and stable ties. New tests verify representative fields, shared
memory rows, cancellation, reduction errors and zero remaining index cursors
after cleanup. Native original and modified libraries built.

In-memory relations, joins and the shared materialization for grouping sets
retain their existing input construction. Stateful aggregates such as
percentiles retain their required per-group inputs. There is no spill or
new query memory limit. High-cardinality group and DISTINCT state need a
separate spill/resource-limit design before claiming safe arbitrary-scale
execution. No full-scale (99,997,497-row) or patched Redis Cloud timings were
collected, and the existing Cloud experiment was not modified.

## Reproduce

Use a dedicated Redis server separate from integration tests. Set REDIS_URI
to that server. Keep the Parquet file and official PostgreSQL create.sql and
queries.sql outside the repository. Build original and modified native
libraries from separate checkouts; the examples use macOS `.dylib` names
(Linux uses `.so`).

```sh
make -C go build
uv run --no-project --with pyarrow --with adbc-driver-manager \
  python benchmarks/aggregate-pages/run.py --phase load \
  --parquet /path/hits-100k.parquet --ddl /path/create.sql \
  --queries /path/queries.sql --driver go/build/libadbc_driver_redis.dylib

uv run --no-project --with pyarrow --with adbc-driver-manager \
  python benchmarks/aggregate-pages/run.py --phase query \
  --parquet /path/hits-100k.parquet --ddl /path/create.sql \
  --queries /path/queries.sql --driver go/build/libadbc_driver_redis.dylib \
  --baseline /path/original/libadbc_driver_redis.dylib --output /tmp/queries-100k.json

# Use the same profile test in the baseline checkout.
cp go/aggregate_pages_profile_test.go /path/original/go/
go -C go test -c -o /tmp/incremental.test
go -C /path/original/go test -c -o /tmp/original.test

uv run --no-project --with pyarrow --with adbc-driver-manager \
  python benchmarks/aggregate-pages/profile.py \
  --baseline /tmp/original.test --worker /tmp/incremental.test \
  --queries /path/queries.sql --output /tmp/heap-100k.json
```

Repeat load/query/profile with `--rows 10000 --table it_212_hits10k` and
separate output paths for the 10K sample. Load refuses to replace an existing
table. Clean up each dedicated table with the runner's `--phase cleanup`,
passing the same fixture arguments and table name; cleanup deletes its rows.
The profiler requires a preloaded table and does not create or drop data.
