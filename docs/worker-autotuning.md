# Aggregate worker autotuning

With both worker options left at `auto`, each database handle starts eligible
aggregate scans with `min(4, GOMAXPROCS)` shared workers. Explicit ceilings
can further reduce the automatic target. This conservative CPU-based starting
point avoids serializing all concurrent work during cold learning. It learns
from normal execution; no
statement is replayed and the application’s `GOMAXPROCS` is never changed.
Automatic exploration is bounded by `min(32, 2 * GOMAXPROCS)`. Go’s current
runtime capacity (including Go-supported container CPU limits) is read at
admission. A capacity change resets the model; existing grants finish normally.

## Objective and regression bounds

The objective is fetched rows per elapsed second for comparable query cohorts.
For a lone query this minimizes its scan latency; for overlapping queries it
measures total throughput including admission wait. Each cohort also records
its maximum query latency including wait, a conservative tail guard rather than
an estimate of p95. Three observations at an allocation are smoothed with an
EWMA (new weight 0.3). A one-worker upward probe is accepted only for a throughput gain
greater than 8% with no more than 15% increase in smoothed maximum latency.
Downward probes also accept throughput within 3% of the baseline to prefer
fewer workers at equivalent performance. Probes switch direction after failure
so a slower Redis deployment can shed excess workers.
Otherwise the previous allocation is restored. A measured throughput loss over
10%, or tail increase over 15%, lowers the allocation and relearns. After four
stable three-observation windows, the policy probes again for changed Redis or
network conditions. These are decision thresholds, not end-to-end performance
guarantees: exploration and admission ordering can cause individual regressions.
This policy seeks a useful local allocation; it does not prove a global maximum.

## Measurements and adaptation

A cohort ends when eligible queries drain, or after eight completions under
continuous traffic. It records actual fetched rows, elapsed time, maximum query
latency, peak outstanding queries (including admission wait), summed scan-fetch
time, Go runtime user plus GC CPU utilization, and Go runtime memory usage as a
fraction of `GOMEMLIMIT` when one is set. Scan-fetch time includes Redis/network
wait and decoding; it is not isolated server response time or network RTT.
There are no background sampler goroutines or extra Redis measurement commands.
CPU utilization of at least 90% or runtime memory pressure of at least 85%
lower the target immediately when a cohort is evaluated.

Changing table, coarse sequence-size bucket, pushed-down predicate, aggregate
input columns, or concurrency invalidates the comparison baseline. The first
cohort in a new regime is discarded and learning restarts at the conservative CPU-based budget. Mixed
workloads, failed/cancelled cohorts, and cohorts spanning allocation changes are
discarded. One model is retained per database handle, so rapidly changing or
mixed workloads may remain conservatively sized. Continuous windows count
completed work; partial queries crossing a boundary add normal sampling noise.

Admission remains FIFO, with queued queries sharing available tokens round
robin. Grants last through scanning, reduction, and cursor cleanup. Decreasing
capacity never preempts existing grants. Cancellation returns tokens even when
it races admission. Tables below 2,048 sequence IDs retain the serial fallback. Eligible scans
cap partitions at one per 1,024 sequence IDs, so a growing learned budget
cannot make a previously admitted small scan bypass coordination. Other
aggregate eligibility checks and exact reducers are unchanged.

## Operator overrides and observability

`adbc.redis.query_workers` sets a per-query ceiling (1–32).
`adbc.redis.query_worker_budget` sets a fixed shared ceiling (positive integer).
Either ceiling constrains automatic allocation. With an explicit shared budget
and automatic per-query workers, the tuner chooses the per-query target while
the coordinator retains the fixed total budget. With both options explicit,
allocation is fixed and measurement is disabled. Changing the budget resets
learning. Automatic grants may be smaller than either ceiling.

Read `adbc.redis.query_worker_tuning` on a database or connection for a JSON
snapshot containing `budget`, `target_workers`, `exploration_ceiling`, `reason`,
`observations`, and `signals`. The existing active/queued counters and connection
`query_workers_last_grant` expose admission decisions. `query_workers` reads the
exploration ceiling; `query_worker_budget` reads effective shared capacity.
Observations count only the current comparison regime. Reasons include runtime
initialization, workload changes, bounded exploration, rollback, and pressure.

Coordination is local to one database handle. Separate handles, processes, and
other Redis clients are not coordinated. Runtime CPU measurements include other
Go application work; memory signals cover the Go runtime and its configured
memory limit, not total process RSS or Redis memory. Host/cgroup memory pressure
without `GOMEMLIMIT` is not directly detected. Server overload is inferred from
throughput/tail regressions, not server-wide INFO polling. Slow or irregular
queries can require more observations to converge.

## Reproducible evaluation

The opt-in `TestAggregateWorkerConcurrencyProfile` checks SQL results and emits
mean and p95 latency, throughput, client CPU, peak heap, allocated bytes, Redis
CPU/memory/commands/network counters, plus per-round grants and tuning snapshots.
Run the [matrix runner](../benchmarks/worker-autotune/matrix.py) against an isolated
Redis. It compares serial, fixed workers/budget, and tuned workers for CPU
capacities 1/2/4, concurrency 1/2/4, and direct versus impaired local links.
The impaired proxy delays each server-read chunk by 2 ms; it is a synthetic link
condition, not a precise RTT emulator. INFO counters measure the entire server;
keep unrelated activity idle. The fixture modifies only `it_worker_profile`.
Inspect the allocation history for convergence; average measurements include
cold learning and probes. This benchmark does not establish bounds across all
Redis deployments. See the archived run for actual results and limitations.
