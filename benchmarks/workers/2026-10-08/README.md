# Single-query driver worker scaling — October 8, 2026

This experiment varies the driver's per-query worker ceiling while keeping
Redis Software at standard QPF, client `GOMAXPROCS=10`, and the shared worker
budget at `auto` (resolved to 10). One SQL query is in flight at a time.
It measures worker scaling at fixed CPU capacity, rather than changing the
number of available CPU cores. The driver implementation and defaults are
unchanged by this benchmark.

## Environment

- Apple M4, 10 logical CPUs, 16 GiB RAM; macOS 26.7.1, arm64.
- Docker Engine 29.8.2, 10 vCPUs and 7.75 GiB RAM. One Redis Software RAM
  database, one shard, 1 GiB database limit, no replica or persistence.
  The [container resource snapshot](container-resources.json) records its
  CPU quota and affinity settings. The server and client share this laptop
  with ordinary host applications.
- Image `redislabs/redis:8.2.0-78.18`, digest
  `redislabs/redis@sha256:a6e52eea1157c836d5eebbea9aae1aa64aaee91522f82791c8799b7b9ec964d9`.
  Redis 8.6.2 and Search 8.6.11.
- Driver source at `5904851`, after PR #229; the aggregate worker implementation
  is from PR #225. Go 1.26.6, Python 3.14.0, PyArrow 25.0.1,
  ADBC driver manager 1.12.0, redis-py 8.1.0.
- Shared library SHA-256:
  `4a82b496fd05fe71732b3ef56b41495f04b103916d5fcd011472cdace81fce46`.
  This is also the binary used in the preceding QPF report.
- Seed-42 fixture: 100,000 `sales` rows × 10 columns, nine indexed columns,
  plus 50 `customers` loaded by `go/examples/bench.py`. Only `sales` is used
  in this comparison. Data is not modified during measurements.

## Method

Seven workloads × five worker settings × five rounds = **175 blocks**.
Each block starts a fresh Python process and ADBC database handle, executes
two warmups, then three measured executions: **525 timed queries**, 15 per
cell. Process startup and connection creation are outside timing. Timing
includes SQL execution and fetching the complete Arrow table into Python.
The reported latency is the median of five block medians; the range of those
block medians is also provided in the resource table.

For each workload, seed 224 shuffles the five settings, then each round
rotates that order by one position. Every setting appears at each position
once. Workload order is shuffled separately each round. The complete schedule
is archived in `results.json`. This balances order effects but does not make
the shared laptop a dedicated benchmark host.

The settings are `adbc.redis.query_workers=1,2,4,8,auto`. The automatic
shared budget stays at 10, and the automatic ceiling resolves to 10. Each
execution verifies the actual grant and its release. Eligible ungrouped
queries receive 1/2/4/8/10 workers. The four controls receive zero grants:
indexed count and group-by execute server reductions; driver grouped AVG
and full scans do not use this aggregate worker pool. A zero grant does
not imply that the entire client process runs on a single CPU core.

Default `aggregate_pushdown=exact` is preserved. Including `AVG(discount)`
on a DOUBLE column requires driver reduction, ensuring the first three
queries exercise the intended path. The filtered variant uses
`WHERE quantity BETWEEN 5 AND 10`. Full SQL and output row counts are stored
in the metadata and [harness](../run.py).

A separate one-worker reference is obtained for every workload. Every block
checks complete results against it, with 1e-12 relative/absolute tolerance
for floats. Small results are additionally checked on every execution.
Full scans verify every execution's row count and a hash of the complete
result at the end of every block, sorted by `order_id` and serialized as
combined-chunk Arrow IPC. Validation is outside the timed region.

The management API verifies effective standard settings before the reference
runs, before every round, and after the last round: zero background Search
workers, `cmp` routing, five connections, and active database status.
The fresh database has QPF inactive with these standard effective settings.
No QPF changes are made. Docker inventory is captured at the same boundaries.
Redis proxy INFO counters are unsuitable for attributing per-query CPU and
are not used for the resource measurements.

## CPU and memory definitions

- **CPU ms/query**: median of block medians of Python `time.process_time()`
  deltas around execution and Arrow fetch. This includes all Python and Go
  threads in the client, including garbage collection, but excludes Redis,
  Docker, process startup and result validation.
- **Mean client CPU cores**: total timed client CPU milliseconds divided by
  total timed wall milliseconds for the cell. A value of 2.0 means two CPUs
  busy on average (200% of one CPU), not two reserved cores or two workers.
- **Process peak RSS**: `getrusage(RUSAGE_SELF).ru_maxrss` in each fresh block
  process, read after timing and before full-scan sorting/hash validation.
  It includes the interpreter, Arrow, Go runtime, warmups and measured queries.
  It is a process high-water mark, not incremental query memory or total
  system memory. Both the median and maximum across five blocks are reported.

## Results

All 175 blocks and 525 timed queries passed result, row-count, and grant
checks. All seven server checks retained the same standard configuration;
only `go-redis-software-1` appeared in the captured container inventories.

Automatic (10) versus one worker reduced median latency by **2.20×** for
ungrouped AVG (656.7 → 298.5 ms), **2.21×** for mixed SUM/AVG/MIN/MAX
(729.9 → 330.3 ms), and **2.06×** for the filtered variant (239.6 → 116.2 ms).
For the mixed aggregate, mean client CPU usage rose from **0.72 to 1.89 busy
cores**, CPU time from **532.9 to 639.2 ms/query**, and median process peak
RSS from **128.6 to 297.0 MiB**. The controls' zero grants confirm that their
latency variation does not reflect aggregate-worker scaling.

The automatic setting follows GOMAXPROCS; it does not search for the fastest
setting. It had the lowest reported median on the three eligible workloads,
but block ranges overlap at some settings. Higher worker counts improve
latency here while increasing CPU work and memory; this does not establish
an optimum for other fixtures or concurrent workloads.

The generated [tables](tables.md) include latency, CPU, observed grants,
process peak RSS, and the range of block medians. The machine-readable
[summary](summary.json) is derived from the [raw measurements](results.json).

## Reproduce

Run from the repository root on macOS or Linux with Docker, Go, uv, curl,
and jq installed. `make redis-up` starts the disposable local Redis Software
database. The fixture command replaces its `sales` and `customers` tables.
Use a new output directory for each run; the harness refuses to overwrite
an existing result file or run against nonstandard QPF settings.

```sh
make -C go redis-up build
uv run --project go/validation python go/examples/bench.py --rows 100000 --repeat 1 > /tmp/worker-fixture.txt
uv run --project go/validation python benchmarks/workers/run.py --cpus 10 --rounds 5 --repeat 3 --warmups 2 --seed 224 --output /tmp/adbc-workers-new
python3 benchmarks/workers/summarize.py /tmp/adbc-workers-new
make -C go redis-down
```

Choose `--cpus` to match the fixed client `GOMAXPROCS` you intend to study.
It does not change Docker's CPU allocation or impose host CPU affinity.
With fewer than eight available budget tokens, explicit ceilings above that
budget receive fewer workers; the raw records report the resolved grants.

The [fixture log](fixture.txt) contains one separately measured ingest
(1.37 seconds, 73,107 rows/s) and a one-repeat fixture sanity run of the older
20-query suite. Those timings are not part of this worker matrix; its delayed
INFO command counts are not used. The earlier [QPF latency/concurrency
report](../../qpf/2026-10-08-adbc/README.md) and [OSS performance snapshot](../../readme-performance/2026-10-08/README.md)
remain available as separate experiments.
