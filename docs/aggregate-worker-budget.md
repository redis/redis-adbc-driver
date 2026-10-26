# Shared aggregate worker budget

Issue [#76](https://github.com/redis/redis-adbc-driver/issues/76) replaces
independent per-query automatic allocation with a coordinator shared by all
connections from one ADBC database handle. Automatic capacity follows current
`GOMAXPROCS`, including Go runtime CPU-limit adjustments. This is process-local
state; independent database handles and processes have independent budgets.

`adbc.redis.query_workers` is a ceiling. `adbc.redis.query_worker_budget` can
reduce aggregate concurrency to leave headroom or explicitly allow a larger
budget for workers that spend time waiting on Redis. Automatic per-query
ceilings still follow `GOMAXPROCS` when the budget is explicitly larger.

Admission is FIFO and grants are non-preemptive. A lone query can borrow all
unused tokens. New arrivals use spare tokens or queue; they do not reclaim
workers from an already running scan. On release, queued queries receive one
worker each in FIFO order before remaining capacity is divided round-robin,
up to their ceilings. Therefore two queries started together can initially run
sequentially if the first has already acquired the whole budget. This favors
single-query latency and avoids interrupting scans, rather than promising equal
shares throughout overlapping queries. Every finite scan releases its grant;
cancelled or timed-out waiters leave the queue, including cancellation racing
with admission. Reductions and cursor cleanup finish before tokens are returned.
A lower new runtime limit or configured budget does not revoke existing grants.
An eligible query granted one token uses the existing serial reducer while
holding that token; unsupported and small-table fallbacks remain outside the
coordinator.

Read-only options expose resolved capacity, active grants, queued queries, and
a connection's last admitted grant. Counters describe reserved workers, so a
worker waiting on network I/O still counts as active. Serial fallbacks and
server-side pushdown retain their existing paths outside this coordinator.

## Reproduce the benchmark

From `go`, against a test Redis with Search:

```bash
REDIS_URI=redis://localhost:16379/0 GOMAXPROCS=4 \
REDIS_WORKER_PROFILE_ROWS=100000 REDIS_WORKER_PROFILE_ROUNDS=10 \
go test -run '^TestAggregateWorkerConcurrencyProfile$' -count=1 -v .
```

The opt-in profile creates and removes only `it_worker_profile`. It issues
`SUM(v), AVG(f), MIN(v), MAX(v)` over BIGINT and DOUBLE columns with aggregate
pushdown disabled. Each of 1, 2 and 4 connections shares one database handle;
each wave starts its queries together and waits for completion. All results
must equal the initial reference result. Latencies include admission wait.
Set `REDIS_WORKER_PROFILE_BUDGET` or `REDIS_WORKER_PROFILE_CEILING` to compare
explicit limits. Increase rows/rounds for larger datasets or steadier tails.

Each `WORKER_PROFILE` JSON record reports mean query latency, p95 using the
nearest-rank method, total throughput, actual process CPU time, CPU utilization
(100% = one logical CPU), sampled peak Go heap, allocated bytes, Redis network
throughput, Redis commands/second, Redis CPU time and Redis memory. It also
records runtime capacity, the resolved budget and each connection's last grant.
The heap sampler runs every 5 ms; it can miss short peaks and does not measure
RSS. Redis INFO counters cover the whole server: use an idle test server for
isolated attribution. INFO and monitoring add a small measurement overhead.

## Local results, 2026-10-08

A local macOS client and Docker Redis, 100,000 synthetic rows, 10 waves,
`GOMAXPROCS=4`, automatic ceiling and budget resolving to 4:

| Concurrent queries | Mean query latency (s) | p95 (s) | Queries/s | Client CPU | Peak heap (MB) | Redis network (MB/s) | Redis commands/s | Redis CPU seconds | Redis memory (MB) |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 0.474 | 0.588 | 2.108 | 95.6% | 184.0 | 33.3 | 36.0 | 2.670 | 39.8 |
| 2 | 0.718 | 1.096 | 2.171 | 97.6% | 199.2 | 34.3 | 37.3 | 5.533 | 39.8 |
| 4 | 1.269 | 2.449 | 2.407 | 103.9% | 128.0 | 38.0 | 40.1 | 9.367 | 39.8 |

MB is decimal. These measurements used an isolated Redis container after the
complete Go suite, so Redis memory includes its leftover test fixtures. They
are local smoke measurements, not a dedicated full-scale ClickBench result.
Two concurrent queries produced similar throughput to one; four improved
throughput by about 14%, while p95 rose from 0.588 to 2.449 seconds. Last grants
in the final wave were `[4]`, `[4, 4]` and `[2, 1, 1, 4]` respectively; these are
per-connection snapshots, not simultaneous grants (the active total remains
bounded by four). Results include admission wait and local CPU/network costs.
They do not establish that using full runtime capacity is best for every
deployment. The automatic policy is full runtime capacity; operators can retain
a fraction through the budget override.

Validation covers runtime capacities 1, 4 and 16, explicit per-query ceilings,
concurrent active-worker limits, FIFO sharing, budget resizing, cancellation
racing grants, queued deadlines, real concurrent aggregate results, statement
cancellation during admission, scan errors returning tokens, and existing
serial fallback cases. Focused tests run under Go's race detector.
