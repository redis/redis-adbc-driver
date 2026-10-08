# README performance refresh: 2026-10-08

This reruns the original [sample benchmark](../../../go/examples/bench.py) on
PR #225's driver code, commit `77976c8`, using the previous README's hardware
and Redis/Search versions. It updates the main README's query/ingest snapshot.
It is not an A/B experiment isolating the shared worker-budget implementation.
The separately measured QPF, row-value and wide-integer benchmarks retain their
own historical settings and results.

## Setup and method

- Apple M4, 10 logical CPUs, 16 GiB RAM; macOS 26.7.1, arm64.
- Docker Desktop: Docker Engine 29.8.2, 10 CPUs, 7.75 GiB reported memory.
- Fresh standalone and three-master cluster containers, no replicas, using
  Redis 8.4.4 / Search 8.4.10. The cached image was
  `redis@sha256:c44528447fa07ed62bdb0c1944cba54f8cad6a4e4a49ada9d4843b5b07d03227`.
  These match the Redis/Search versions in the previous published table.
- Go 1.26.6; Python 3.14.0; PyArrow 25.0.1; ADBC driver manager 1.12.0;
  redis-py 8.1.0. The built shared library's SHA-256 is recorded in JSON.
- The original seed-42 dataset: 100,000 sales rows, 10 columns, 50 customers.
  Default aggregate pushdown and streaming settings. Automatic worker ceiling
  and shared budget both resolve to 10; no worker overrides or tuning.
- Standalone and cluster runs execute sequentially. Each query has one warm-up
  followed by five timed executions. Timings include execution and fetching
  the complete Arrow result into Python. Ingest is one timed load per server.
- Redis command counts come from `INFO commandstats`, summed across all three
  cluster masters for the cluster run. The harness excludes INFO and command
  categories averaging less than 0.5 calls/run, and rounds the displayed totals.
  Cluster totals include internal shard fan-out. Pipelined `HMGET`s count as
  separate Redis operations even though they share network round trips.

The servers were isolated from existing benchmark data. The laptop and Docker
VM still share host resources with other applications and containers. For
example, the cluster equality-EXISTS query ranged from 1.80 to 8.08 seconds,
with a median of 2.31 seconds; the median does not hide this spread in the raw
files. This is a local snapshot rather than a production throughput forecast.

## Results

Bulk ingest: standalone **1.03 s / 96,734 rows/s**; three-shard cluster
**0.73 s / 137,441 rows/s**.

| Query | Rows out | Standalone median (ms) | Cluster median (ms) | Standalone commands/run | Cluster commands/run |
|---|---:|---:|---:|---:|---:|
| point lookup by __rowid | 1 | 1.4 | 1.0 | 2 | 2 |
| indexed filter + top 10 | 10 | 4.8 | 4.9 | 12 | 15 |
| indexed range COUNT | 1 | 6.3 | 4.3 | 2 | 5 |
| LIKE prefix COUNT | 1 | 79.7 | 81.4 | 5 | 29 |
| interval range COUNT | 1 | 1.2 | 1.4 | 2 | 5 |
| GROUP BY, index reduce | 6 | 7.3 | 4.1 | 2 | 5 |
| GROUP BY, driver (AVG double) | 6 | 604.2 | 583.2 | 100,015 | 100,119 |
| GROUP BY expression, driver | 5 | 704.6 | 581.5 | 100,015 | 100,119 |
| GROUP BY DATE_TRUNC, driver | 12 | 1890.7 | 1940.3 | 100,015 | 100,123 |
| non-indexed filter (scan) | 1 | 267.5 | 286.8 | 15 | 118 |
| fetch 10% of rows | 10,000 | 102.7 | 62.0 | 10,006 | 10,019 |
| full scan, all columns | 100,000 | 1116.6 | 1027.5 | 16 | 125 |
| selective join (index lookup) | 8 | 33.3 | 43.9 | 6 | 21 |
| unfiltered join (hash join) | 6 | 464.0 | 517.9 | 22 | 134 |
| correlated subquery (50 rows) | 50 | 67.6 | 101.1 | 54 | 212 |
| IN (SELECT …), half the rows | 1 | 1270.2 | 1642.1 | 150,043 | 150,204 |
| correlated EXISTS (semi-join) | 1 | 1731.8 | 2307.5 | 150,548 | 152,217 |
| correlated EXISTS, no inner rows | 1 | 4.4 | 5.5 | 5 | 11 |
| UNION of two filters | 1,750 | 54.1 | 60.6 | 1,953 | 1,966 |
| view with pushed filter | 1 | 20.2 | 22.9 | 4 | 7 |

All 20 query labels and output row counts match between standalone and cluster.
The raw files contain min/median/max timings and the dominant command counts:
[standalone](standalone.txt), [three-shard cluster](cluster.txt).
[Structured results and environment metadata](results.json) contain every row.
Dependency-installation messages preceding the standalone server header were
excluded from its saved benchmark output.

Current narrow-projection scans fetch fields that the index cannot represent
exactly using pipelined `HMGET`. Grouped driver aggregates therefore show
100,000 `HMGET`s per run, and the 10,000-row projection shows 10,000. This is
existing scan behavior, outside the new worker coordinator's grouped-query
eligibility; it explains why the previous README's cursor-only command-count
explanation needed updating. The dedicated [worker concurrency profile](../../../docs/aggregate-worker-budget.md)
measures eligible ungrouped aggregates with a separate synthetic fixture.

## Reproduce

From `go`, build the checked-out driver and run the unchanged benchmark:

```bash
make build
REDIS_URI=redis://localhost:26379/0 uv run --project validation --with redis \
  python examples/bench.py --rows 100000 --repeat 5
REDIS_URI=redis://localhost:27001/0 uv run --project validation --with redis \
  python examples/bench.py --rows 100000 --repeat 5
```

These URIs were isolated test servers. Use a disposable server: the harness
replaces `sales` and `customers` and creates/removes `bench_shipped`.
For the recorded server version, use the image digest above. The standalone
container maps host port 26379 to 6379. The cluster follows `go/compose.yaml`'s
three-node startup method, substituting ports 27001–27003, mapped unchanged to
the host, with `--cluster-announce-ip 127.0.0.1`, persistence disabled and the
Search module loaded on every node. Keep CPU/memory allocations comparable.
The temporary benchmark containers were removed after recording the results.
