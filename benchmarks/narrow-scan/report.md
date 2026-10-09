# Narrow exact scans: issue #211

Measured October 7, 2026, using the first 100,000 rows of the official
`hits_compatible/hits.parquet`, all 105 columns and default indexes. The
fixture was extracted from the existing ClickBench runner without changing
its active Cloud benchmark. The SQL files came from that runner's ClickBench
checkout, `29b09614bf8ffcbd46ac83f64804cb060422c15d`.

The modified driver avoids `LOAD *` for narrow managed-table projections
that need values the index cannot load exactly. It loads keys and safe
fields through the cursor, then fetches unsafe fields, their missing-value
markers and `__rowid` using pipelines of at most 1,000 HMGET commands. A
missing `__rowid` skips a deleted row. Sort aliases remain intact, including
when LIMIT tie correction reads a run again. Adopted tables keep `LOAD *`
because they have no universal row-existence field.

The selection rule requires at least half the table's columns to be omitted.
This is a conservative heuristic for avoiding extra round trips on wide
projections, not a measured optimum for every schema or network. Exact-safe
projections retain their existing cursor-only path. No option or change to
`aggregate_pushdown=exact` is required.

## End-to-end query results

Both libraries were built from the same base commit
`53c7bbf0b04404445674fa2a947960b49474feeb`, with the modified library including
this patch. The host was macOS arm64, Go 1.26.6, and Docker with 10 virtual
CPUs. Redis was an isolated standalone `redis:8.6.2` container accessed
through localhost; these are **local measurements, not Redis Cloud reruns**.
The complete Go suite used a separate Redis container on the same Docker
host. Other local services were present, so timings can vary with host load.

Each query ran three times per library, with strategy order alternated
between trials. A fresh process and RESP2 ADBC connection was used for each
query. Timing includes execution and fetching the Arrow table, and excludes
connection setup, fixture reads and reference validation. Hot latency is the
smaller of trials 2 and 3, matching the pilot convention.

| Query | Original driver | Narrow scan | Speedup |
|---|---:|---:|---:|
| Q4 `AVG(UserID)` | 6.146 s | 1.628 s | 3.77× |
| Q5 `COUNT(DISTINCT UserID)` | 4.818 s | 1.900 s | 2.54× |
| Q16 UserID grouping, top 10 by count | 4.078 s | 1.810 s | 2.25× |

All 18 trials passed independent Python references computed from the source
UserID column. Q4 uses Python integer sums before division and a relative
floating tolerance of 1e-12; Q5 uses exact distinct cardinality. Q16 checks
exact counts, descending order, unique returned users, the cutoff, and that
all groups strictly above the cutoff are present; tied users at the cutoff
may vary. Raw trials and returned values are in [query-results.json](query-results.json).

## Scan profile

The Go profiler reads the same UserID projection through both strategies,
without SQL reduction or Arrow conversion. It alternates strategy order,
discards one warm-up pair and reports medians of three measured pairs. It
checks row count and a wrapping int64 checksum in every trial. The separate
SQL validations above provide exact reference checks beyond that checksum.

| Metric for 100,000 rows | `LOAD *` | Narrow exact |
|---|---:|---:|
| Actual socket reply bytes | 330,584,399 | 10,969,061 |
| Total allocated bytes | 2,057,193,232 | 289,327,920 |
| Allocation count | 42,310,072 | 2,902,004 |
| Cursor pages, including final empty page | 54 | 11 |
| Scan elapsed time | 5,398 ms | 1,633 ms |
| Cursor execution/read/parse time | 5,232 ms | 364 ms |
| HMGET execution, parsing and merge time | 0 ms | 1,126 ms |
| Row decode and checksum time | 190 ms | 144 ms |

Reply traffic fell **96.7%** and allocation volume **85.9%**. Allocations
include go-redis response parsing, raw row maps, HMGET commands and decoded
values. `TotalAlloc` measures cumulative allocation, not peak RSS or retained
heap. Socket counters include actual Redis replies, including protocol framing,
and exclude request bytes. Cursor time includes server work, network and
response parsing; it does not isolate server CPU. Fetch time includes the
extra HMGET stage, which is now the largest scan component. The narrow path
uses 100 HMGET pipeline executions of up to 1,000 commands each. Stage medians
are computed separately, so they need not sum to the total median.

An additional `FT.PROFILE` sample of 1,000 rows reported 12.635 ms across the
two `LOAD *` loader processors versus 1.039 ms for the narrow cursor loader,
and total server profile times of 19.457 ms versus 2.068 ms. This single
sample excludes the subsequent HMGET server work and is not a whole-query
server speedup. Redis documents these processor times in milliseconds in
[FT.PROFILE](https://redis.io/docs/latest/commands/ft.profile/).

Raw measurements are in [scan-results.json](scan-results.json) and
[server-profile.json](server-profile.json).

## Correctness and remaining scope

The complete Go integration suite passed against standalone Redis. Targeted
regressions passed against a three-shard Redis cluster and standalone RESP2.
They exercise exact BIGINT boundaries (including values
above 2^53 and signed int64 extremes), mixed safe/unsafe fields, NULLs,
missing defaults, explicit NULL markers, renamed fields, residual predicates,
sort ties at LIMIT/OFFSET, deletion after the cursor matches a row, and
cancellation before and between pipeline batches. Existing adopted-table
coverage runs in the complete suite. The profiler traverses 100K rows across
multiple cursor pages. Native original and modified libraries both built.

The first full-suite attempt overlapped the synthetic profile on the same
server and failed the collection-discovery test because that profile created
an extra table. The profile was moved to a separate server; the clean
full-suite rerun passed. The synthetic results were not used in the tables
above.

Full-scale (99,997,497-row) timings and patched 40-shard Redis Cloud timings
remain unmeasured. The existing full-scale Cloud benchmark was left running
with its original library. The local Docker memory limit cannot hold that
full fixture. This work does not resolve the separate driver-side aggregate
materialization issue #212: aggregation still retains decoded input rows.
Two-stage reads may observe concurrent writes between cursor loading and
HMGET; queries do not supply snapshot isolation.

## Reproduce

Use a dedicated standalone test Redis, separate from integration tests. Keep
fixture data outside the repository. Build the original library from the
base commit in a separate checkout and build this patch with `make -C go build`.
Set `REDIS_URI` to the dedicated server, e.g. `redis://127.0.0.1:16381/0`.
The examples below use macOS library filenames; Linux uses `.so`.

```sh
uv run --no-project --with pyarrow --with adbc-driver-manager \
  python benchmarks/narrow-scan/run.py --phase load \
  --parquet /path/to/hits-100k.parquet --ddl /path/to/create.sql \
  --queries /path/to/queries.sql --driver go/build/libadbc_driver_redis.dylib

(cd go && REDIS_SCAN_PROFILE=1 REDIS_SCAN_PROFILE_TABLE=it_211_hits \
  go test -run '^TestNarrowScanProfile$' -count=1 -v)

uv run --no-project --with pyarrow --with adbc-driver-manager \
  python benchmarks/narrow-scan/run.py --phase query \
  --parquet /path/to/hits-100k.parquet --ddl /path/to/create.sql \
  --queries /path/to/queries.sql --driver go/build/libadbc_driver_redis.dylib \
  --baseline /path/to/original/libadbc_driver_redis.dylib --output /tmp/query-results.json

uv run --no-project --with redis python benchmarks/narrow-scan/server_profile.py \
  --index idx:public:it_211_hits --output /tmp/server-profile.json

uv run --no-project --with pyarrow --with adbc-driver-manager \
  python benchmarks/narrow-scan/run.py --phase cleanup \
  --parquet /path/to/hits-100k.parquet --ddl /path/to/create.sql \
  --queries /path/to/queries.sql --driver go/build/libadbc_driver_redis.dylib
```

The `load` phase creates a dedicated `it_211_…` table and refuses to replace
an existing table. The `cleanup` phase drops that table and its rows. Without
`REDIS_SCAN_PROFILE_TABLE`, the Go profiler generates its own synthetic
105-column table and removes it afterwards; those results must be labelled
synthetic.
