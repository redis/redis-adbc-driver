# Redis Software: SQL/Arrow and QPF, October 8, 2026

This report preserves the single-client QPF latency comparison and the
concurrent QPF throughput experiment. The primary README latency table now
uses the [driver worker-scaling experiment](../../workers/2026-10-08/README.md).
Both measure complete ADBC SQL execution and Arrow fetch on Redis Software,
including the client work that QPF cannot accelerate.

## Environment

- Apple M4, 10 logical CPUs, 16 GiB RAM; macOS 26.7.1, arm64.
- Docker Engine 29.8.2: 10 vCPUs, 7.75 GiB memory. One fresh Redis Software
  container, one RAM database, one shard, 1 GiB database limit, no replica,
  no persistence. Only this container appeared in the sampled Docker inventory.
  Ordinary host applications still share the laptop; this is not a production forecast.
- Image `redislabs/redis:8.2.0-78.18`, digest
  `redislabs/redis@sha256:a6e52eea1157c836d5eebbea9aae1aa64aaee91522f82791c8799b7b9ec964d9`.
  Database Redis 8.6.2, Search 8.6.11.
- Driver code at `66aba73` (same driver implementation as PR #225), Go 1.26.6,
  Python 3.14.0, PyArrow 25.0.1, ADBC driver manager 1.12.0, redis-py 8.1.0.
  Shared library SHA-256: `4a82b496fd05fe71732b3ef56b41495f04b103916d5fcd011472cdace81fce46`.
- Seed 42 sample fixture: 100,000 sales rows × 10 columns and 50 customers.
  Automatic worker ceiling and shared budget both resolve to 10. Concurrent
  clients clone connections from one ADBC database handle, sharing that budget.

## Method and limits

The [20-query latency table](../../../go/examples/bench.py) uses one warm-up and five timed executions per
query at each setting, in standard/2×/4× order. Timing includes fetching the
whole Arrow table. Each setting has one five-run block, so small differences
should not be interpreted as stable gains. Ingest was measured once at
standard QPF: **1.10 seconds, 90,888 rows/s** ([initial run](standard.txt)).

The [concurrent SQL/Arrow harness](../adbc.py) runs 90 trials: five queries,
two client counts per query, three QPF settings, and three rounds. The setting order rotates:
standard/2×/4×, 4×/standard/2×, 2×/4×/standard. Every client warms up twice,
then requests for five seconds; throughput includes draining outstanding
requests. Small index queries use 1 and 16 clients. Larger driver/transfer
queries use 1 and 4 to bound memory. Slow trials have few requests, so their
p95 values are descriptive rather than precise tail estimates.

The management API confirms QPF, Search workers, routing policy, and connection
limits after every update, then the benchmark waits two seconds. Standard uses
`active=true, scaling_factor=0`, Search workers 0 (main-thread execution),
`cmp` routing, and five connections. 2×/4× use 3/6 workers, `mnp`, and 32
connections. Setting `active=false` alone retained expanded settings on this
image. An [initial discarded concurrency run](initial-run-superseded.json)
exposed that problem; none of its trial ratios are used here. The three
single-client latency blocks were completed before its first reset to standard,
so those blocks retain their original valid settings. The corrected concurrent
matrix was rerun in full. The older direct-command report has the same reset
problem in its recorded metadata and is now marked superseded.

The five concurrent workloads' complete results are compared against the
starting reference at each setting (sorted by order_id for full scans, 1e-12 tolerance for floats).
Timed iterations also verify row counts. All 90 trials completed without
errors. QPF was restored to its original standard setting in a finally block.

Docker CPU/memory/inventory samples are saved roughly every 10 seconds.
They do not isolate individual processes or measure client CPU. Redis proxy
INFO counters are delayed/aggregated: the raw latency logs preserve the old
harness's command-count column, but it is unsuitable for attributing commands
to these queries. The same limit applies to INFO deltas in raw trial records;
neither is used to explain speedups. QPF adds Search compute; Python, Go,
proxy, decoding, and Arrow transfer all still share this host.

## Single-client SQL latency

| Query | Rows | Standard ms | 2× ms | 4× ms |
|---|---:|---:|---:|---:|
| point lookup by __rowid | 1 | 1.5 | 1.4 | 1.2 |
| indexed filter + top 10 | 10 | 3.2 | 2.8 | 3.3 |
| indexed range COUNT | 1 | 7.2 | 5.8 | 5.8 |
| LIKE prefix COUNT | 1 | 88.4 | 91.9 | 85.8 |
| interval range COUNT | 1 | 1.4 | 1.4 | 1.3 |
| GROUP BY, index reduce | 6 | 7.6 | 8.4 | 7.8 |
| GROUP BY, driver (AVG double) | 6 | 592.8 | 626.6 | 604.7 |
| GROUP BY expression, driver | 5 | 680.4 | 664.7 | 673.9 |
| GROUP BY DATE_TRUNC, driver | 12 | 774.3 | 863.7 | 817.1 |
| non-indexed filter (scan) | 1 | 416.0 | 366.3 | 356.3 |
| fetch 10% of rows | 10,000 | 78.2 | 80.8 | 67.6 |
| full scan, all columns | 100,000 | 887.4 | 1067.4 | 981.2 |
| selective join (index lookup) | 8 | 37.1 | 36.4 | 51.9 |
| unfiltered join (hash join) | 6 | 460.7 | 496.1 | 480.3 |
| correlated subquery (50 rows) | 50 | 54.2 | 52.8 | 54.9 |
| IN (SELECT …), half the rows | 1 | 1002.2 | 1100.3 | 984.1 |
| correlated EXISTS (semi-join) | 1 | 1197.6 | 1183.6 | 1237.1 |
| correlated EXISTS, no inner rows | 1 | 2.0 | 2.0 | 1.9 |
| UNION of two filters | 1,750 | 52.4 | 54.1 | 51.8 |
| view with pushed filter | 1 | 25.3 | 23.5 | 24.0 |

## Concurrent throughput

Values are medians of three trials; ratios divide each median by standard.

| Query | Clients | Standard QPS | 2× QPS | 4× QPS | 2× / standard | 4× / standard |
|---|---:|---:|---:|---:|---:|---:|
| index_group | 1 | 108.62 | 119.68 | 116.89 | 1.10× | 1.08× |
| index_group | 16 | 145.96 | 391.02 | 602.60 | 2.68× | 4.13× |
| index_count | 1 | 186.11 | 184.18 | 188.01 | 0.99× | 1.01× |
| index_count | 16 | 259.07 | 612.84 | 1037.63 | 2.37× | 4.01× |
| driver_group | 1 | 1.74 | 1.71 | 1.72 | 0.98× | 0.99× |
| driver_group | 4 | 3.47 | 4.22 | 4.17 | 1.22× | 1.20× |
| driver_reduce | 1 | 3.44 | 4.50 | 4.88 | 1.31× | 1.42× |
| driver_reduce | 4 | 3.82 | 4.08 | 4.25 | 1.07× | 1.11× |
| full_scan | 1 | 1.17 | 1.11 | 1.18 | 0.95× | 1.01× |
| full_scan | 4 | 1.71 | 2.24 | 2.17 | 1.31× | 1.27× |

## Variability and tail latency

Each cell is the QPS range across three trials / median trial p95 in ms.

| Query | Clients | Standard | 2× | 4× |
|---|---:|---:|---:|---:|
| index_group | 1 | 103.35–130.82 / 11.0 | 118.47–125.79 / 9.7 | 111.31–123.76 / 9.8 |
| index_group | 16 | 124.24–150.68 / 176.4 | 325.18–471.80 / 47.1 | 540.09–615.92 / 38.4 |
| index_count | 1 | 181.69–188.45 / 6.5 | 182.25–195.11 / 6.5 | 182.54–204.74 / 5.7 |
| index_count | 16 | 255.96–262.64 / 68.8 | 589.58–655.41 / 31.4 | 919.27–1088.29 / 22.9 |
| driver_group | 1 | 1.65–1.78 / 588.6 | 1.56–1.72 / 613.7 | 1.68–1.80 / 618.2 |
| driver_group | 4 | 3.24–3.62 / 1318.3 | 3.64–4.23 / 1019.0 | 3.81–4.34 / 1120.6 |
| driver_reduce | 1 | 3.21–4.01 / 315.6 | 4.09–4.96 / 263.2 | 3.95–4.97 / 233.3 |
| driver_reduce | 4 | 3.56–4.11 / 1340.4 | 3.50–4.24 / 1688.6 | 4.16–5.12 / 1205.3 |
| full_scan | 1 | 1.10–1.23 / 871.6 | 1.01–1.12 / 1018.5 | 1.07–1.19 / 900.8 |
| full_scan | 4 | 1.69–2.00 / 2692.1 | 2.11–2.56 / 2009.2 | 1.00–2.21 / 1980.7 |

## Reproduce

Run on a disposable environment. Loading replaces `sales` and `customers`.
Allow enough CPU/memory for QPF: standard uses one Search CPU, 2× requires
three, and 4× requires six, plus Redis/host headroom. See
[Redis's QPF sizing guidance](https://redis.io/docs/latest/operate/oss_and_stack/stack-with-enterprise/search/query-performance-factor/).
Do not run other test suites during the comparison.

From the repository root:

```bash
make -C go redis-up build
REDIS_URI=redis://localhost:6379/0 uv run --project go/validation --with redis \
  python go/examples/bench.py --rows 100000 --repeat 5
REDIS_URI=redis://localhost:6379/0 uv run --project go/validation --with redis \
  python benchmarks/qpf/adbc.py --output /tmp/adbc-qpf-results \
  --seconds 5 --rounds 3 --latency-bench
make -C go redis-down
```

[Raw trials and environment](results.json), [latency data](latency.json),
[summary data](summary.json), and raw latency logs
([standard](latency-qpf-0.txt), [2×](latency-qpf-2.txt), [4×](latency-qpf-4.txt))
retain the complete measurements. The
[October 6 direct-command experiment](../report.md) and
[earlier OSS snapshot](../../readme-performance/2026-10-08/README.md)
are historical comparisons with different conditions.

## Default-environment validation

After measuring, the Software container was recreated before testing.
`make -C go test` passed for both the driver and CLI packages. `make -C go validate`
reported 360 passed, 8 skipped, and 3 subtests passed. Software blocks
protocol ACL user creation and `CONFIG RESETSTAT`; those Go administrative
checks skip explicitly, while SQL correctness runs there. The affected ACL,
row-read counter, and adoption-error tests also passed on the OSS three-shard
compatibility target. The benchmark's failure path was checked on an empty
database and restored an originally inactive standard configuration, including
workers/routing/connections.
