# Managed writer admission tail baseline

2026-11-14; driver runtime b904cf8d with test-only instrumentation. This is Go ADBC SQL execute/acknowledgement timing, including cleanup, rather than Python/C-boundary latency or Redis server CPU. No runtime optimization is included and these numbers are not a before/after improvement claim.

Post-measurement scope clarification: the harness creates an idle execution pool before timing to install hooks, so cold pool-object creation is excluded. Each measured `ExecuteUpdate` still acquires/registers its normal per-statement execution transport and finishes it before returning. It does not pre-acquire an execution context. Instrumentation adds per-call category/timeline allocation and locking; scheduling/GC may differ from uninstrumented/native-C calls. Historical raw labels and source hashes are unchanged.

Verified Redis Software: active 4× QPF, six Search workers, `mnp` routing, 4 GiB database, certificate-verified TLS and RESP3. The runner set `GOMAXPROCS=4`. Infrastructure identities and connection details are private. No ClickBench was run.

One warmup plus three measured trials rotate 1/4/8 writers. Every trial starts from 1,000 rows and performs 144 total writes (48 INSERT, 48 UPDATE, 48 DELETE), interleaved per writer against one shared table with disjoint row IDs. All 12 trials passed complete ordered final-row/type/value checks: 1,728 acknowledged writes, zero errors, conflicts, caller retries or missing admission observations. Warmup is retained in the artifacts and excluded from the following pooled measured statistics. Each table cell has 144 observations across three trials.

| Writers | Operation | p50 ms | p95 ms | Admission p50 ms | Admission p95 ms |
|---|---|---:|---:|---:|---:|
| 1 | INSERT | 136.01 | 196.62 | 113.99 | 172.83 |
| 1 | UPDATE | 43.24 | 111.69 | 15.84 | 86.60 |
| 1 | DELETE | 36.28 | 49.63 | 12.95 | 24.05 |
| 4 | INSERT | 71.68 | 2159.46 | 28.87 | 2132.84 |
| 4 | UPDATE | 48.37 | 617.18 | 14.95 | 579.66 |
| 4 | DELETE | 67.39 | 1179.68 | 23.24 | 1154.02 |
| 8 | INSERT | 83.17 | 3548.26 | 50.01 | 3510.69 |
| 8 | UPDATE | 56.15 | 793.11 | 13.25 | 716.07 |
| 8 | DELETE | 54.23 | 2113.43 | 16.91 | 2091.21 |

Admission means operation start through confirmed successful `admit_ingest`, including catalog and target validation. Off-transport time includes timers, scheduling, Go work and background-context pin release; it is not a direct timer-sleep measurement.

| Worst measured operation | Total ms | Admission ms | Admission off-transport ms | Acquire attempts | Admission pipelines containing FT.INFO ms | GC-associated STW ms |
|---|---:|---:|---:|---:|---:|---:|
| 1 writer | 231.73 | 196.27 | 113.58 | 7 | 3.36 | 0.00 |
| 4 writers | 4007.38 | 3983.87 | 3349.83 | 56 | 51.53 | 0.81 |
| 8 writers | 7551.37 | 7500.77 | 6781.15 | 95 | 73.44 | 0.45 |

The next priority is admission waiting and fairness. At four/eight writers, 84%/90% of the worst operation's admission time was outside the observed foreground command calls. Removing repeated physical-index proof pipelines would address about 1.3%/1.0% of those admission times. The control script admits whichever eligible writer reaches it first; the client backs off up to 100 ms after releasing its pin. Investigate a bounded, cancellable admission queue or notification design that permits compaction, rather than assuming fewer proof calls will resolve these tails. Any candidate still needs identical-workload before/after evidence and native end-to-end checks.

The [full samples](observations.json.gz) retain per-operation command categories/timings and separate production maintenance-client timelines. [Trial summaries](summary.json) retain every trial and p95-tail command overlap. Overlapping maintenance duration is correlation, not causal attribution or CPU, and concurrent client durations can exceed wall time when summed. Go GC association sums STW pause durations for GC cycles completed during an operation; it is not exact pause overlap or concurrent GC CPU. All measured trial GC rings were complete. This harness does not measure delivery of local compactor wake notifications.

The initial attribution attempt incorrectly asserted bare `*redis.Cmd` while production sends its `onceCmd` wrapper. Cross-review caught zero admission values; that run was stopped and excluded as invalid instrumentation. The corrected unit covers the production wrapper, and the complete rerun above has no missing admission observations.

Run the opt-in diagnostic only in an exclusive, verified Software 4× QPF slot:

```sh
REDIS_MANAGED_TAIL_PROFILE=true go test -count=1 -p 1 -timeout 20m -run '^TestManagedInterleavedWriterTailProfile$' -v .
python3 ../benchmarks/managed-writer-tail/summarize.py /path/to/private-test.log
```

Set `REDIS_SNAPSHOT_URI` privately to the disposable verified database. `REDIS_MANAGED_TAIL_WRITES` accepts total writes in multiples of 24 between 48 and 1440. Review [SDLC validation](../../docs/sdlc/changes/managed-writer-tail-attribution/validation.md).
