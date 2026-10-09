# Aggregate worker autotuning

With both worker options left at `auto`, each database handle starts eligible
aggregate scans with `min(32, GOMAXPROCS)` shared workers. Explicit ceilings
can further reduce the automatic target. This CPU-sized starting
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
an estimate of p95. Six comparable observations form a robust median window. Throughput and
maximum-latency windows with median absolute deviation above 12% are rejected.
Each one-worker candidate is compared with the retained allocation measured
both before and after the probe. Reference throughput must remain within 10%
and maximum latency within 15%; environmental drift rejects the candidate.
Upward candidates need a throughput improvement exceeding the greater of 3%
and a margin based on observed relative deviation. This margin is a heuristic,
not a statistical confidence interval. Candidate maximum latency must stay
within 10% of the better reference window. Downward candidates may retain
throughput within 2% to prefer fewer workers. Accepted directions continue
one worker at a time; a rejection restores the retained allocation and doubles
the holding period from 48 to at most 192 comparable cohorts.

A sustained slowdown starts a paired downward probe; it cannot repeatedly
ratchet the retained allocation downward without checking a candidate. Later
probes reassess changed Redis/network conditions. Learning losses are bounded
in allocation step and probe duration, not in end-to-end latency or throughput.
These decision thresholds do not guarantee the external performance gate.
The policy seeks a useful local allocation and does not prove a global maximum.

## Measurements and adaptation

A cohort ends when eligible queries drain, or after eight completions under
continuous traffic. It records actual fetched rows, elapsed time, maximum query
latency, peak outstanding queries (including admission wait), summed scan-fetch
time, Go runtime user plus GC CPU utilization, and Go runtime memory usage as a
fraction of `GOMEMLIMIT` when one is set. Scan-fetch time includes Redis/network
wait and decoding; it is not isolated server response time or network RTT.
There are no background sampler goroutines or extra Redis measurement commands.
Three consecutive CPU-utilization signals of at least 90% prefer a downward
probe and suppress upward exploration. Runtime memory pressure of at least 85%
reduces the current target immediately and starts a fresh baseline.

Changing table, coarse sequence-size bucket, pushed-down predicate, aggregate
input columns, or concurrency changes the comparison regime. The first cohort
is discarded. Each handle caches up to eight inactive/recent workload models in
insertion order alongside its active model. Returning workloads resume their
retained allocation with a fresh baseline; partially measured probes are never
resumed. New workloads start CPU sized. Failed/cancelled, mixed-workload, or
allocation-transition cohorts are discarded. Outstanding queries carry workload
and allocation-generation leases, so continuous windows recover once old work
finishes instead of retaining a sticky mixed marker. Changing the shared budget
invalidates outstanding learning samples even if its allocation stays the same.
Continuous windows count completed work; partial queries crossing a boundary
add sampling noise. Quickly alternating or mixed workloads can remain CPU sized.

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
`observations`, `retained_workers`, `candidate_workers`, `phase`,
`cached_models`, and `signals`. Candidate denotes the latest attempted candidate;
target denotes the allocation currently used, including a live probe. The existing active/queued counters and connection
`query_workers_last_grant` expose admission decisions. `query_workers` reads the
exploration ceiling; `query_worker_budget` reads effective shared capacity.
Observations count only the current comparison regime. Reasons distinguish initialization, restored workload models, candidate probes,
reference verification, rejection for drift/noise/tail, holding, and pressure.

Coordination is local to one database handle. Separate handles, processes, and
other Redis clients are not coordinated. Runtime CPU measurements include other
Go application work; memory signals cover the Go runtime and its configured
memory limit, not total process RSS or Redis memory. Host/cgroup memory pressure
without `GOMEMLIMIT` is not directly detected. Server overload is inferred from
throughput/tail regressions, not server-wide INFO polling. Slow or irregular
queries can require more observations to converge.

## Reproducible evaluation

The opt-in `TestAggregateWorkerConcurrencyProfile` checks every SQL result against
an independent serial executor. It separates cold learning from warmed execution
and emits per-query latency, throughput, client CPU/heap/allocations, process-lifetime
RSS, Redis CPU/memory/network/commands, and allocation/grant history. Fixtures are
named `it_worker_profile_<rows>` and can be reused only after indexed-count and
sequence verification. Continuous clients cycle independently within each phase.

Use the [controlled runner](../benchmarks/worker-autotune/controlled/README.md)
for native CPU/memory-limited clients, randomized repeated policies, OSS/Software,
32,768-row matrix, 100,000-row/continuous spot checks, and acknowledged
2-to-1 CPU Redis-capacity transitions. It predeclares the gate
and records missing trials as infrastructure failures separately from performance
regressions. The manual workflow archives evidence even when the gate fails.
The delayed condition adds 2 ms per complete aggregate/cursor response with a
native framed proxy; it is not RTT. Redis INFO measures the entire server and
observer CPU still consumes the client's quota. Keep unrelated activity idle.

The [initial evaluation](../benchmarks/worker-autotune/README.md) preserves the
older three-sample policy's uncontrolled, cold-inclusive results. A passing local
gate would not establish bounds across all deployments or production workloads.
