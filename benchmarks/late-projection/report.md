# Ordered residual SELECT: late projection (#214)

Measured 2026-10-08 against main `66fc8ad451f88905dd77b619826eb0c9c43acc88`
(includes #211–#213). Public ClickBench fixture: first 100,000 rows of
`hits_compatible/hits.parquet`, official PostgreSQL 105-column schema;
ClickBench commit `29b09614bf8ffcbd46ac83f64804cb060422c15d`.
Local Apple M4, Go 1.26.6, Redis 8.6.2/Search 8.6 in Docker, standalone RESP2.
These are local pilot measurements, not the issue's Redis Cloud numbers or
a full-scale ClickBench result. No new indexes or storage fields were added.

## Native driver comparison

Three alternating trials per strategy/case, fresh native ADBC processes and
connections. Time includes ExecuteQuery plus Arrow fetch, excludes connection
setup and independent Python validation. Reply bytes count actual worker
Redis traffic through a loopback proxy. Queries are SELECT * ordered by
EventTime, LIMIT 10; the OFFSET case uses OFFSET 10. Raw results retain all
trials and SQL in `query-results.json`.

| Predicate/case | Matches | Median seconds, before → after | Reply MiB, before → after |
|---|---:|---:|---:|
| Q24 `%google%` | 0 | 5.751 → 0.734 | 322.531 → 22.756 |
| `%produkty%`, OFFSET 10 | 89,286 | 0.241 → 0.165 | 6.034 → 2.297 |
| `%gaw.ru%`, low selectivity | 1,151 | 0.329 → 0.147 | 12.114 → 2.297 |
| Absent sentinel | 0 | 5.735 → 0.721 | 322.531 → 22.756 |

Q24's hot time (minimum of trials 2–3) is 5.271 → 0.680 seconds. Its reply
transfer falls 92.9%. This fixture has **no Google URL matches**; Q24 here
exercises the complete residual scan and avoids every wide output fetch.
The additional matching cases validate selection and full-row output.

All 24 comparisons passed an independent reference built from Parquet:
all 105 fields are cast to the SQL result types, result cardinality and
ordered EventTime sequence match, every full row belongs to the qualifying
source multiset, and every strictly interior row of the selected timestamp
window is present. Boundary membership among equal EventTime values is
unspecified by SQL and may differ. No checksum-only validation is used.

## Stage profile

Separate compiled Go workers walk the production ordered scan until the
first ten qualifying rows, or EOF. Three alternating trials per case,
without OFFSET. `stage-results.json` contains raw results. Command hooks
measure FT.AGGREGATE/FT.CURSOR time and HMGET pipelines; the remaining client
and other time includes decoding, residual evaluation, tie correction, and
other commands. It is not an isolated LIKE or decode microbenchmark.

| Case | Cursor rows before → after | Cursor replies before → after | Output HMGET rows after | Median cursor seconds before → after | Median client/other seconds before → after |
|---|---:|---:|---:|---:|---:|
| Q24 | 100,000 → 100,000 | 54 → 11 | 0 | 3.438 → 0.480 | 1.374 → 0.239 |
| Matching | 1,869 → 10,000 | 1 → 1 | 10 | 0.085 → 0.055 | 0.041 → 0.063 |
| Low selectivity | 3,738 → 10,000 | 2 → 1 | 10 | 0.153 → 0.057 | 0.079 → 0.064 |
| Absent | 100,000 → 100,000 | 54 → 11 | 0 | 3.537 → 0.427 | 1.406 → 0.236 |

The narrower scan uses larger cursor pages, so it can decode more candidate
rows before stopping on high-selectivity queries. Matching-case projection
HMGET takes approximately 0.8–0.9 ms for ten rows here. Wide transfer and
105-field decoding now occur only for those ten outputs; no-match cases
materialize none. Narrow scans still sort all candidates in Redis and
perform exact SQL residual evaluation, without pushing LIMIT before it.

`server-profile.json` is a separate one-shot instrumented FT.PROFILE sample
of 1,000 outputs after sorting 100,000 candidates. The wide pipeline reports
Sorter 239.36 ms and two Loader stages 7.94/16.96 ms; the narrow pipeline
reports Sorter 27.01 ms and Loader 1.25 ms. FT.PROFILE rejects cursors, so
this sample substitutes LIMIT after sorting. Different warm state and
instrumentation prevent interpreting these two sorter times as a controlled
sorting speedup; the operator structure still sorts the full candidate set.

## Correctness and practical limits

Full Go suite passed on a clean standalone Redis instance (163.4 s).
Targeted tests passed standalone and three-shard cluster, RESP2 and RESP3:
BIGINT and nanosecond timestamp tie correction before LIMIT/OFFSET, ascending
and descending order, escaped LIKE, ILIKE, NULLs, no matches, exhausted
OFFSET, LIMIT 0, streamed/eager results, narrow cursor payload, deleted-row
refill, and cancellation. Existing rounded-sort tests also passed on cluster.

The optimization needs an ordered managed-table scan with LIMIT, a residual
predicate needing at most half the table's columns, and output columns
beyond those inputs. Driver sorting, windows, correlated subqueries, and
adopted HASHes retain the existing path. Each cursor page still decodes its
predicate inputs. Large rounded sort ties may require extra narrow reads.
As with existing cursor scans, concurrent writes are not a snapshot: output
HASHes are fetched later than predicate evaluation. Deleted output HASHes
are skipped and replaced by later qualifying rows without consuming LIMIT.

## Reproduce

Use isolated Redis and the fixture/SQL paths; never run integration tests
against the loaded benchmark server (discovery tests expect their own data).
The loader refuses to replace an existing table. The existing loader's
reserved table prefix explains the `it_213_issue214` name.

```sh
make -C go build
# Build baseline dylib from main 66fc8ad in a separate checkout.
export REDIS_URI=redis://127.0.0.1:16386/0
uv run --no-project --with pyarrow --with adbc-driver-manager --with redis \
 python benchmarks/exact-distinct/run.py --phase load \
 --driver go/build/libadbc_driver_redis.dylib \
 --parquet /path/hits-100k.parquet --ddl /path/create.sql \
 --queries /path/queries.sql --table it_213_issue214
uv run --no-project --with pyarrow --with adbc-driver-manager --with redis \
 python benchmarks/late-projection/run.py \
 --driver go/build/libadbc_driver_redis.dylib \
 --baseline /path/baseline/go/build/libadbc_driver_redis.dylib \
 --parquet /path/hits-100k.parquet --output /tmp/query-results.json
cp go/late_projection_profile_test.go /path/baseline/go/
go -C go test -c -o /tmp/late.test
go -C /path/baseline/go test -c -o /tmp/baseline.test
python3 benchmarks/late-projection/profile.py --worker /tmp/late.test \
 --baseline /tmp/baseline.test --output /tmp/stage-results.json
# Writes server-profile.json in the current directory.
uv run --no-project --with redis python benchmarks/late-projection/server_profile.py
# Drop only this fixture, using the same loader arguments and --phase cleanup.
```
