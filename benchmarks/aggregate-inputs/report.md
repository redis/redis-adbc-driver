# Shared integer aggregate inputs (#215)

Measured 2026-10-08 against main `5c92d8dc0c4c6cf0a47965d5297c05b585f4971c`
(includes #211–#214), using first 100,000 public ClickBench
`hits_compatible/hits.parquet` rows and the official PostgreSQL 105-column
schema. ClickBench commit `29b09614bf8ffcbd46ac83f64804cb060422c15d`.
Local Apple M4, Go 1.26.6, standalone Redis 8.6.2/Search 8.6 in Docker,
RESP2, default exact pushdown. These are local pilot measurements, not
Redis Cloud timings or a full-scale ClickBench ranking.

## Implementation and proof boundary

Driver aggregation compiles pure integer inputs into a lazy DAG. Column
references, integer literals, and checked `+`, `-`, `*` share identical nodes
within a row. Q30's 90 inputs share the ResolutionWidth lookup; nested
integer expressions can share their entire common subtree. An epoch changes
for every row, including group changes; cached values retain no row maps.

Roots are evaluated in the original aggregate order. Child evaluation stays
left-to-right and occurs before NULL propagation, so NULL on the left does
not hide an error on the right. Filters, DISTINCT, extended aggregates,
casts, functions, subqueries, parameters, noninteger inputs and unsupported
operators keep the existing evaluator. Every aggregate still updates its
own existing exact accumulator, in the same row order. Types and duplicate
output names are preserved. No index, storage or public option changes.

Affine SUM rewrites were considered and left out: `SUM(x+c)` can overflow
on an individual integer input even when the final sum would cancel, and
floating-point `f+c` rounds per row. The regression with f = 1e16, -1e16, 1
has SUM(f+1) = 2, while SUM(f) + COUNT(f) = 4. The typed path checks each
integer operation; decimal and floating arithmetic remain unchanged.

## Validation

Full Go suite passed after the final code change on clean standalone Redis
(156.9 s). Targeted three-shard RESP2/RESP3 tests passed. Coverage includes
NULL and empty input, grouping, identical nested integer subexpressions,
row-cache invalidation, duplicate output names, signed boundaries checked
against independent big.Int arithmetic, NULL/error evaluation, aggregate
error order, filters that skip invalid inputs, exact decimals and floating
rounding/cancellation. Both scalar and typed arithmetic use the same checked
integer helper; the independent boundary oracle checks that helper too.

All native benchmark outputs are checked by column position against Python
integer sums from the public Parquet file, including all 90 Q30 values.
This avoids losing duplicate SQL output names in a dict. Native timing
includes ExecuteQuery and Arrow fetch, excludes connection setup and
reference computation. CPU is the process user+system delta over that same
query interval. Separate fresh processes and connections run three
alternating trials for each strategy/width. The 1/10 cases are SUM(x+1)
through SUM(x+n); the 90 case is the unmodified official Q30 (x through x+89).

## Native results

Median of three trials; raw outputs, CPU and RSS are in `query-results.json`.

| Aggregate inputs | Execute + fetch seconds, baseline → typed | Query CPU seconds, baseline → typed |
|---|---:|---:|
| 1 expression SUM | 0.539 → 0.505 | 0.473 → 0.450 |
| 10 expression SUMs | 0.632 → 0.581 | 0.563 → 0.445 |
| Official Q30, 90 SUMs | 1.653 → 0.942 | 1.483 → 0.838 |

Q30 improves 43.0% in median execute+fetch latency and 43.5% in query CPU.
Its hot latency (minimum of trials 2–3) is 1.649 → 0.942 seconds. The
single-expression latency improvement is small (6.2%) and the shared-node
benefit grows with the number of related expressions. Transfer is unchanged:
both drivers read the same narrow ResolutionWidth input through the cursor.
All 18 native trials passed the independent integer reference.

## CPU stages and allocations

`stage-benchmarks.txt` measures decoded rows with x = 1920, three runs of
300 ms per case. Each operation evaluates n expressions x+1 through x+n.
The scalar control uses the current checked arithmetic helper, and the
typed control uses the same compiled evaluator as production. Evaluation,
accumulator updates, and their combination are separately timed; these are
CPU microbenchmarks, not end-to-end server measurements.

| Inputs per decoded row | Evaluation ns/row, scalar → typed | Exact accumulator updates ns/row | Evaluation + updates ns/row, scalar → typed |
|---|---:|---:|---:|
| 1 | 97.1 → 37.5 | 18.2 | 117.3 → 57.7 |
| 10 | 947.3 → 273.4 | 171.6 | 1,214 → 451.8 |
| 90 | 8,402 → 2,434 | 1,617 | 10,576 → 4,008 |

Every final stage benchmark reports 0 B/op and 0 allocs/op. Compilation and
accumulator initialization occur before the stage timer; their costs remain
included in the separate production profile. Per-row checks and independent
accumulator updates remain necessary work, so the stages are not an affine
shortcut.

`load-profile.txt` isolates the actual 100K-row one-column cursor and row
decoding path, without expression or accumulator work: median 0.361 s,
226,569,456 allocated bytes and 2,100,208 allocations. That path is unchanged.
The separate production Go workers in `heap-results.json` profile the whole
aggregation after planning and before Arrow conversion, in fresh processes:

| Inputs | Allocated bytes, baseline → typed | Allocation count, baseline → typed |
|---|---:|---:|
| 1 | 226,619,648 → 226,621,264 | 2,100,303 → 2,100,315 |
| 10 | 226,636,280 → 226,650,080 | 2,100,374 → 2,100,395 |
| 90 | 226,787,288 → 226,890,904 | 2,100,877 → 2,100,893 |

Q30 adds about 101 KiB and 16 allocations per query for its compiled input
DAG, while adding no allocation per evaluated row. Loading/decoding dominates
allocation volume. The Go profile's median Q30 time is 1.561 → 0.757 s;
it excludes planning/Arrow and should not be substituted for native latency.
There are 18 production allocation profiles, in addition to the 18 native
trials. Sampled heap/RSS are retained in raw results, not presented as a
stable memory improvement from three trials.

## Reproduce

Use separate isolated Redis instances for integration tests and the fixture.
The existing loader's reserved prefix explains `it_213_issue215`. It refuses
to replace an existing table. No public data or credentials are checked in.

```sh
make -C go build
# Build the baseline driver from main 5c92d8d in a separate checkout.
export REDIS_URI=redis://127.0.0.1:16386/0
uv run --no-project --with pyarrow --with adbc-driver-manager --with redis \
 python benchmarks/exact-distinct/run.py --phase load \
 --driver go/build/libadbc_driver_redis.dylib --table it_213_issue215 \
 --parquet /path/hits-100k.parquet --ddl /path/create.sql --queries /path/queries.sql
uv run --no-project --with pyarrow --with adbc-driver-manager --with redis \
 python benchmarks/aggregate-inputs/run.py \
 --driver go/build/libadbc_driver_redis.dylib \
 --baseline /path/baseline/go/build/libadbc_driver_redis.dylib \
 --parquet /path/hits-100k.parquet --queries /path/queries.sql \
 --output /tmp/query-results.json
# Both revisions already include the generic TestExactDistinctProfile worker.
go -C go test -c -o /tmp/typed.test
go -C /path/baseline/go test -c -o /tmp/baseline.test
python3 benchmarks/aggregate-inputs/profile.py --worker /tmp/typed.test \
 --baseline /tmp/baseline.test --queries /path/queries.sql --output /tmp/heap-results.json
go -C go test -run '^$' -bench '^BenchmarkAggregateInputStages$' \
 -benchmem -benchtime=300ms -count=3
REDIS_INPUT_LOAD_PROFILE_TABLE=it_213_issue215 /tmp/typed.test \
 -test.run '^TestAggregateInputLoadProfile$' -test.v -test.count 3
# Drop only this fixture with the loader's same arguments and --phase cleanup.
```
