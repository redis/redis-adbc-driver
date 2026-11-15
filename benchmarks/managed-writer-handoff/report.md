# Bounded process-local managed writer handoff

Date: 2026-11-14. Baseline runtime `d7c0978f`; measured candidate `03e04002`.
Status: **opt-in measured candidate, Linux native confirmation complete; human review pending**.
The existing native server admission and protocol 17 remain authoritative.
The public database option `adbc.redis.writer_admission_queue` defaults to
`false`; `true` opts newly opened eligible connections into this scheduling
tradeoff. It is an option within managed mode, not another mode. Default false
skips identity fingerprinting, the extra CA-bundle read and the registry path.

The full comparison improves contended whole-workload completion, p95/p99 and
command counts. **Medians and several operation means increase substantially.**
This is a fairness/throughput tradeoff, not a blanket latency improvement or a
selected default speedup. One-writer throughput is essentially unchanged, with
mixed finite percentile differences.

[Exact source/binary/harness provenance](provenance.json),
[executed patch](executed-candidate.patch.gz),
[complete long samples](observations.json.gz),
[complete comparison](comparison.json), [pilot summary](pilot-summary.json),
[pilot samples](pilot-observations.json.gz), [checks](checks.json), and
[SDLC](../../docs/sdlc/changes/managed-writer-handoff-20261114/intent.md).

## Scheduling contract

A local FIFO turn suppresses redundant local polling and hands off after ordinary
bounded pin cleanup. Requests use opaque static routing/principal/database/TLS
and logical-target identities. Dynamic/unrepresentable credentials, custom
routing/TLS, identity aliases, other processes and bounded overflow retain the
original server-controlled path. No global or cross-process FIFO is promised.
Waiters release pins before waiting; every handoff repeats fresh catalog, target
and native admission checks. There is no SQL/input replay, cached authority,
new server command or storage format.

The registry caps 256 targets and 1,024 requests, including owners. These bound
registry cardinality, **not arbitrary caller-owned context memory**. Active
cancellation registration can indirectly retain the parent execution context.
Release detaches and clears its stop closure and unlinks the turn; an already
running callback can finish concurrently. The registry has no explicit
client/catalog/credential fields. Cancellation can release a local turn before
server cleanup, but the next writer must still satisfy native server admission.

## Full matched workload

Both persistent test binaries were built and frozen before the pilot and reused
unchanged for the long run. Long blocks were Bwarm/Cwarm/B0/C0/C1/B1/B2/C2, rotating
1/4/8 writers within each block. Every trial starts with 1,000 rows and interleaves
288 disjoint INSERT/UPDATE/DELETE writes. All 24 trials, including warmups, passed
complete typed final-row/value checks: **6,912 acknowledged writes, zero failures,
conflicts, retries or missing admission observations**. Warmups are retained but
excluded below. Each measured operation/concurrency/build cell has 288 samples.

Throughput divides all measured successful writes by summed complete-trial wall
time. These finite closed-loop completion rates are not saturated server capacity.
The overall mean weights all operations equally; per-operation means and the
large median regressions remain visible.

| Writers | Throughput B→C ops/s | Change | Foreground commands/ack B→C | Maintenance commands/ack B→C | Overall mean operation ms B→C |
|---|---:|---:|---:|---:|---:|
| 1 | 12.346→12.323 | -0.2% | 95.30→96.00 | 146.09→146.53 | 80.99→81.15 |
| 4 | 12.329→13.746 | +11.5% | 142.67→122.28 | 176.37→163.08 | 279.87→289.07 |
| 8 | 12.041→13.520 | +12.3% | 194.76→120.35 | 209.62→166.99 | 533.43→583.61 |

| Writers | Operation | p50 B→C ms | p95 B→C ms | p99 B→C ms | Mean B→C ms |
|---|---|---:|---:|---:|---:|
| 1 | INSERT | 144.59→160.03 | 243.18→235.38 | 303.48→250.47 | 139.02→151.13 |
| 1 | UPDATE | 47.21→46.02 | 148.68→123.38 | 176.75→185.86 | 62.45→50.73 |
| 1 | DELETE | 38.41→37.95 | 73.43→100.15 | 201.78→190.39 | 41.51→41.58 |
| 4 | INSERT | 88.81→293.82 | 1961.54→465.40 | 3307.00→531.07 | 381.63→294.49 |
| 4 | UPDATE | 53.45→272.63 | 1268.81→413.74 | 3877.00→521.29 | 244.53→282.99 |
| 4 | DELETE | 60.19→281.49 | 866.93→466.49 | 2819.52→504.00 | 213.47→289.73 |
| 8 | INSERT | 70.12→579.92 | 4078.79→798.72 | 10523.39→870.69 | 721.87→558.41 |
| 8 | UPDATE | 62.77→591.68 | 3084.05→829.08 | 8410.36→1490.45 | 560.44→605.53 |
| 8 | DELETE | 55.72→568.93 | 2150.43→793.02 | 4797.53→1458.96 | 317.96→586.88 |

Percentiles use nearest rank. Per-trial durations, maxima, acquire counts and
admission/off-transport distributions remain in the complete comparison JSON.
The separate pilot used 48 writes/trial, trial 0 only, B/C/C/B: all 12 records and
576 acknowledgements validated. It had no explicit warmup; its 32 samples per
operation/build make p99 the maximum. Do not combine pilot and long distributions.

## Safety and resource context

Focused race units passed FIFO/isolation/overflow/cancellation and cleanup races,
static TLS identity, and custom-identity bypass. Software race checks passed
independent database-handle handoff, queued connection Close, SQL/Arrow admission,
lost replies and cancellation. An outside-registry caller could not bypass a
coordinated native owner, consume NEXTVAL before admission, or replay an unknown
reply. This models the actual uncoordinated path; it is not an OS-subprocess
performance or global-fairness test. Post-measurement expanded TLS coverage is
identified separately in the checks record.

Every invocation independently verified Redis Software with active/effective
4× QPF, six Search workers, mnp/32, one 4 GiB RAM shard, noeviction, strict TLS and
RESP3. The runner used Go 1.27.3, GOMAXPROCS=4 and production maintenance workers.
The server container allocation was four CPUs/8 GiB; Docker VM has 18 logical
CPUs. Selected read-only [cgroup counters](block-resource-counters.json) were
sampled outside trial clocks, before/after each long block. All eight blocks have
zero additional throttled periods. Usage includes all container/server activity
and setup/cleanup; it is not query CPU or an additive wall-time decomposition.
The [coordinator's final environment observation](../performance-round3/2026-11-14/environment.json)
will supplement the original run gates without replacing source/binary hashes.
Operational identities, credentials, logs and binaries remain private.

All processes, clients, transports and workers ended, and independent verification
observed an empty fixture after both pilot and long comparisons, before releasing
the exclusive slot. Root owns final fixture cleanup. Reconstruct the exact
measured source against `d7c0978f` with
`gzip -dc executed-candidate.patch.gz | git apply`; the patch includes the original
measured test coverage. Post-run source changes add the explicit default-false public option, clarify a
helper comment and add 16 TLS bypass cases. This option gate is a functional
post-measurement change: the Go measurements describe the preceding always-on
prototype, not a new timed run of the final default or enabled build. Actual
default/enabled safety checks and a newly frozen native pair are required;
measured/final hashes and scopes remain distinct in provenance.

## Separate Linux native confirmation

The [native wrapper](native_run.py) uses `REDIS_WRITER_QUEUE=true` to pass the
public option and verify database GetOption on every candidate connection.
Baseline false passes no new option. Requested/verified selection is recorded
in metadata. It selects the unchanged standard managed and
redis-py INSERT/UPDATE/DELETE scenarios: 1/4 threads, 32 operations, one warmup
and three repetitions, ordinary statement lifecycle, persistent client objects,
and all original typed/final-state oracles. It exercises independently created
database handles in one process. Process-pool clients cannot share this queue.

The [complete Linux native ABBA confirmation](native-linux/README.md) now records
actual baseline `d7c0978f` versus explicitly enabled candidate `0884e3c8`, with
all 192 records/6,144 operations validated. Four-writer INSERT/UPDATE/DELETE
completion throughput increases from 73.12/69.73/69.03 to 80.07/83.09/81.70 ops/s,
while p95 falls from 199/284/195 to 76/70/74 ms. **Pooled medians increase from
6.7/7.6/7.1 to 47.6/46.3/46.7 ms, and means increase from roughly 35 to 47 ms.**
One-writer results are close/mixed; all redis-py controls and per-pair drift remain
visible. This validates an opt-in fairness/completion tradeoff, not a default or
general latency speedup. The public option remains false by default.

The earlier Go timings above used macOS's published-port route. They remain
historical and must not be relabeled as Linux timings. A Linux control did not
reproduce the ~13 ms post-proof delay, so that delay has no established general
driver/Software cause. The Linux native dataset has its own frozen sources,
binaries, topology, controls and complete oracles.

The original raw `automatic_retries=0` label is preserved but is not a measured
wire retry/reconnect counter. Post-run scope notes distinguish no caller retry
loops, Linux redis-py's verified zero command retry budget, and connection
management/reconnect behavior. Client/cursor object setup is outside timing;
lazy first-operation transport/execution-pool work remains included, and warmup
trials do not prewarm future trials' new client objects. See the separate native
report for all limitations. No upstream issue, ClickBench or deployment is claimed.
