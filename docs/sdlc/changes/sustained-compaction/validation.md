# Sustained maintenance validation

Date: 2026-11-06. Issue [#337](https://github.com/redis/redis-adbc-driver/issues/337),
delivery [PR #348](https://github.com/redis/redis-adbc-driver/pull/348).
Status: local final-source acceptance complete; delivery gates recorded on the PR.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and [design](design.md).

## Sources and correctness

Preceding snapshots: `c6baae32d77fd13575f0fc46defaaf761f699df4`.
Unconditional experiment: `778f0a4c3301664cacfc2247c5642bef71633a7e`.
Final production: `e5fa7bdc`, with the verified full native build revision in the
results manifest, identifies the background-only implementation. It preserves
serial reads for small jobs and leaves the ordinary scanner unchanged.
Mutable performance reference: `f4d5b787e8c28c036baf49de9190c4e6f83ca2e8`.
The three initial native libraries embed clean VCS identities and Go 1.27.3, built with
the repository Makefile/version flags before matched timing. Later observer and
offered-load changes affect only the Python harness and documentation.

The following initial correctness checks cover `778f0a4c`, not the later gated
refinement. All live checks use the owned Redis Software fixture, active/effective 4x QPF,
strict TLS and noeviction. Three configured primary shards share one Software
node/proxy; managed snapshot keys use one hash slot. This is not cross-shard
snapshot or new promotion/persistence coverage. Recovery incident #345 remains
separate and unresolved.

The mutable reference retains its own native row-key/index layout, which can
distribute rows across the configured shards. Managed snapshots place their
rows with the catalog in one hash slot. This compares the actual supported
storage models on the same fixture, not identical per-shard engine workloads.
Their consistency guarantees and physical key/index costs also differ.

- Focused read-pipeline/atomic/directory/index races: 16 pass events, no skips.
  The original individual-read refusal fails on unchanged production in 0.37 s.
- Complete Software snapshot race selection: 572 pass events / 58 explicit
  skips, all six executable packages pass; root package 367.509 s. No failure or
  race report. Preserved-native/Cluster/special-principal/profile skips remain
  skips, not compatibility evidence.
- Endpoint-free unit races: 1,211 pass events / 1,081 expected integration/profile
  skips; all six executable packages pass, root 52.948 s.
- Actual Software-principal SQL SELECT grant races: 22 pass events, no skips.
- Native snapshot C/ADBC: 19 tests, no skips, 10.840 s.
- Complete official native ADBC validation: 565 tests plus three subtests pass,
  eight capability skips, 75.90 s (JUnit records 568 passing cases / eight skips).

The page/cardinality checks retain their original behavior for ordinary readers.
Pipeline-level and per-command read failures in either rank or page phase leave
copy progress/output unchanged. The full snapshot selection additionally covers
pinned streams, exact visibility/tombstones, newer tails and DDL, stale-owner
takeover, unknown copy outcomes, cancellation, writer admission and reference-safe
reclamation. Existing bounds, pacing, GC and mutation protocols are unchanged.

## Final-source correctness and attribution

The final native library embeds clean `e5fa7bdc` / Go 1.27.3. The production Go
files remain unchanged after that commit. A later test-only strengthening uses
`UPDATE ... RETURNING` in the small serial-job regression and passes separately.

- Complete Software snapshot races: **573 pass events / 58 explicit skips**;
  all six executable packages pass, root **365.639 s**.
- Endpoint-free unit races: **1,211 pass events / 1,082 expected skips**;
  all six executable packages pass, root **53.625 s**.
- Focused compaction races: **17 pass events / no skips**. The added exact
  `RETURNING`-form regression separately passes **two events / no skips**.
- Actual Software-principal SQL SELECT grant races: **22 pass events / no skips**.
- Native snapshot C/ADBC: **19 tests / no skips**, **10.735 s**.
- Complete native ADBC: **565 tests plus three subtests / eight capability skips**,
  **74.95 s**.

No failure or race report occurs. These counts describe separate selections,
including parent/package events; they are not an independent total. The broader
suite retains actual Cluster/preserved-native/profile/special-permission skips
as skips. CI for the final delivery head remains a separate gate.

The ordinary `snapshot_delta.go` file is byte-for-byte identical to `c6baae32`.
Small jobs use the original serial rank/lazy-page sequence without a preparation
helper call. Only inputs with more than 64 declared source entries invoke bounded
prefetch. Its local page proof preserves the original offset, completion,
cardinality, row identity and visibility requirements.

The isolated final dense fixture completes four pass events / no skips. It
checks all 20,000 exact BIGINT/payload rows and the unrelated 20,000-row table.
The profile runs after all other local tests/builds exit. Compare these single
controlled observations, not statistical native throughput:

| Observation | Preceding `c6baae32` | Final `e5fa7bdc` |
|---|---:|---:|
| Paced complete job ms | 4,386.954 | 3,906.055 |
| Unpaced complete job ms | 2,009.996 | 1,643.810 |
| Paced copy step call ms | 2,515.891 | 1,886.662 |
| Unpaced copy step call ms | 1,926.309 | 1,563.085 |
| Copy-phase client calls | 7,226 | 5,345 |
| Copy steps | 314 | 314 |

This removes **1,881 client calls (26.03%)** in the profiled copy phase with
unchanged command counts and bounds. The final whole-job observations are about
11.0% lower paced / 18.2% lower unpaced. Pacing, setup/final checks, scheduling and
native phases remain distinct; do not compound these percentages with #336.
Raw profiles preserve every phase/category and the rejected unconditional source.

## Measurement corrections and negative evidence

The first smoke's approximate storage probe encountered a legitimately retired
index between FT._LIST and FT.INFO. The harness handles the existing typed
missing-index response only in that statistics probe; query errors and driver
assertions are unchanged. Its failed numeric record is retained.

Initial measurements sampled backlog after full-row validation and started the
drain clock after the final storage probe. These gave maintenance extra time
before observation. The corrected harness takes an atomic, read-only bounded
backlog sample immediately after each timed window, then validates all rows and
samples storage. Drain elapsed time begins at the final completed write and
includes validation/probing. The canceled initial series and earlier exploratory
samples remain separate with their original harness hashes. They are not
relabeled as corrected observations or successful complete processes.

An unpaced corrected-observer attempt completes its snapshot processes at
28.84 preceding / 27.37 candidate writes/s: an observed negative candidate case,
not evidence of a general sparse-write gain. Its mutable process fails in the
third window with a connection reset while reading table metadata, after two
fully validated windows. A separate clean mutable reproduction fails the same
way. No caller retries are used. Incomplete processes are not successful native
comparisons, and their failed-window throughput is not retained as a pass.

After the failures, the fixture remains active with effective QPF4/noeviction,
zero container restarts/OOM kills and zero rejected connections. This health
sample does not identify a cause. The resets are not declared fixed, random or
a proven Redis defect. A separate 60-second mutable observation with an explicit
40-start/s ceiling completes all windows and row checks. The final matched
comparison uses that same ceiling for every source and records actual pacing
and delivered rates; it does not measure mutable peak throughput.

## Timing and storage interpretation

Every timed window includes complete SQL/publication and complete RETURNING
fetch. Every returned ID/value is independently checked, including BIGINT above
2^53. Full final row/value/NULL/multiplicity checks run between windows and after
the drain observation. No SQL/input retry, manual compaction, sweep or forced
foreground copy is used.

Individual latency samples cover execute plus complete fetch. Delivered
throughput uses the whole window, including local expected-value checks and any
explicit offered-load pacing; it is not the inverse of latency or mutable peak
capacity.

The workload is windowed sustained offered load, not an uninterrupted soak.
Between-window full fetches and storage scans create explicit quiet gaps. Their
durations and immediate end-of-load backlog are retained, so later low backlog
is not substituted for pending work at load stop. The process census checks
known local compiler/test/profile processes every 500 ms; it does not prove the
whole host has no unrelated activity. Only the owned Software container runs.

Native key memory is the deduplicated SCAN/MEMORY USAGE sum while maintenance
continues; deleted keys can disappear during observation. Search memory fields
sum native FT.INFO fields across observed indexes; fields overlap and must not
be added together into a new total. These are approximate native allocations,
not RSS, allocator accounting, network bytes or synchronized retained snapshots.
An index count is the FT._LIST observation; retirement during INFO is allowed.

Drain completion means no eligible driver metadata/data queues, compaction job
or candidate and only the current manifest. Mutable storage has no such driver
maintenance and its drain comparison is not applicable. Neither case proves all
server-internal Search GC or allocator memory has returned to the OS. First
observed empty state is an upper bound at the reported cadence, including final
validation/probing time, not an exact instant of physical memory release.

No full feature/performance parity or stable general write speedup follows
from the controlled profile alone.


## Final repeated native comparison

[All samples](native-samples.json.gz), [sources/settings and summaries](results.json),
[all rejected/diagnostic observations](experiment-samples.json.gz),
[complete phase profiles](phase-profiles.json), and
[numeric correctness checks](validation-checks.json) are retained.

The final matched comparison contains **18 isolated processes / 25,631 complete
operations**, six processes per workload in preceding/candidate/mutable/mutable/
candidate/preceding order. Four windows per process span at least 60 seconds of
active offered load. Sparse and mixed modes use two 20,000-row tables; dense
mode updates all rows of two 1,024-row tables. The common ceiling is 40 statement
starts/s, with actual pacing and delivered rates recorded. All processes pass
complete independent results and eligible-work drain checks. Caller retries are
zero. The four additional unpaced A/B/B/A sparse processes contain **7,299
complete operations**, with all values and drains checked independently.

Redis Software **8.2.6-25 / database Redis 8.8.0**, effective **4x QPF**, strict TLS
and noeviction are verified; the image digest is unchanged before the final
series. Apple M5 Max, 18 logical host CPUs, 36 GiB host memory; Docker 18 CPUs /
25,420,967,936 bytes. Exact image digest, Python/Arrow/manager versions and every
clean Go 1.27.3 library hash are in `results.json`. No local builds/tests/profiles
run during measured processes. Both 500 ms process censuses have zero known
competitor violations; only the owned Software container runs.

| Workload | Source | Delivered ops/s | p50 ms | p95 ms | p99 ms |
|---|---|---:|---:|---:|---:|
| sparse | Preceding | 30.134 | 27.82 | 45.52 | 64.82 |
| sparse | Final | 30.265 | 27.68 | 45.05 | 64.56 |
| sparse | Mutable reference | 32.968 | 8.64 | 10.93 | 12.51 |
| mixed | Preceding | 23.038 | 27.28 | 89.87 | 346.20 |
| mixed | Final | 23.999 | 26.96 | 62.58 | 334.16 |
| mixed | Mutable reference | 33.725 | 8.68 | 10.98 | 12.23 |
| dense | Preceding | 3.824 | 257.20 | 313.39 | 332.00 |
| dense | Final | 3.863 | 248.45 | 314.47 | 331.59 |
| dense | Mutable reference | 31.391 | 31.34 | 40.53 | 45.19 |

Observed final delivered-rate changes are **+0.43% sparse, +4.17% mixed and
+1.02% dense** against the preceding snapshot source. These are two processes
per source/workload, not statistical evidence of a stable general speedup.
Sparse behavior is essentially flat after removing the ordinary-reader change
and preserving the original small-job reads. Dense p95 and maximum latency are
slightly worse in this finite comparison; mixed cleanup can take longer. Every
negative case and per-process/window value remains visible.

The final unpaced confirmation, using identical current harness bytes for both
sources, measures **30.238 / 30.502 writes/s** and **27.766 / 27.506 ms medians**,
preceding / final. This does not reproduce the unconditional experiment's large
median regression. Its p95 is **45.576 / 44.504 ms**. These four processes remain
separate from the ceiling-limited series; no samples are pooled across them or
with the rejected implementation.

### Backlog, storage and cleanup

The following observations include immediate post-load backlog before quiet
validation/probe gaps. Memory ranges are approximate post-window native
allocations across both processes. Key bytes are converted to MiB; Search uses
its reported total-index-MB field, not a sum of overlapping memory fields.

| Workload | Snapshot source | Max segments/table | Max candidates | Native key MiB range | Native total-index MB field range | Max observed driver drain s |
|---|---|---:|---:|---:|---:|---:|
| sparse | Preceding | 7 | 2 | 12.13–12.14 | 17.95–18.02 | 6.01 |
| sparse | Final | 7 | 2 | 12.14–12.14 | 17.95–18.02 | 6.73 |
| mixed | Preceding | 8 | 2 | 12.21–12.52 | 18.03–18.34 | 6.95 |
| mixed | Final | 8 | 2 | 12.26–12.46 | 18.10–18.33 | 8.52 |
| dense | Preceding | 4 | 1 | 1.52–2.22 | 2.35–3.57 | 5.06 |
| dense | Final | 4 | 1 | 1.56–2.21 | 2.54–3.57 | 5.15 |

Both sources reach the existing eight-segment limit during mixed churn; the
limit and caller admission/cancellation behavior remain unchanged. Final
immediate data-GC queue maxima are **3 sparse / 6 mixed / 4 dense**. All final
eligible-work queues become empty, no job/candidate remains, only the current
manifest remains, and each table has fewer than four segments after the drain.
The final maximum observed drain is **8.53 s matched / 10.30 s unpaced**, within
the 30-second target. Some final-source drain observations are slower than the
preceding source; no cleanup speedup is claimed.

Validation plus storage sampling creates quiet gaps up to **4.65 s** in the
matched series. They are excluded from delivered-rate windows, included in the
post-load drain bound, and retained explicitly. These finite windowed results
exercise many maintenance cycles and expose bounded sampled backlogs; they do
not prove an uninterrupted long-duration steady state or indefinite churn
convergence. Mixed physical tails change over the horizon, and per-window rates
show that variation rather than hiding it in one median.

The mutable reference retains roughly **4.53 MB key allocations** for the
40,000-row sparse/mixed datasets and **0.23 MB** for dense, versus the larger
snapshot key footprints above. Its observed native total-index-MB fields range
about **14.96–15.02 sparse**, **14.91–14.96 mixed**, and **14.74–33.98 dense**.
Those engine allocations, schema/key layouts, shard distributions and consistency
contracts differ; neither the key/index fields nor their sum establish total
resident/retention memory parity. Server-internal index GC is not measured as a
driver queue. Full mutable performance/storage parity remains unestablished.

### Acceptance and delivery checkpoint

Every #337 acceptance item is mapped: complete phases/dependencies and bounds in
`phase-profiles.json`; supported pipelining/upstream proposal evaluation in
[design](design.md); preservation regressions and full/native/actual-principal
checks in `validation-checks.json`; matched before/after/reference samples,
percentiles, immediate backlog, native allocations and cleanup lag in
`native-samples.json.gz` / `results.json`. All failed/canceled/rejected-source
records remain in `experiment-samples.json.gz` with original source/harness
identities. No new mutation protocol or unbounded maintenance machinery is added.

Final delivery review, owned cleanup, final-head hosted CI and merge/issue
closure are recorded on [PR #348](https://github.com/redis/redis-adbc-driver/pull/348)
and [#337](https://github.com/redis/redis-adbc-driver/issues/337). This committed
checkpoint records local acceptance, not a claim that pending delivery gates
already succeeded. #345 remains separate; no release tag/assets are requested.

### Delivery review and owned cleanup

The agent delivery review covers the net production delta, immutable source
references and identity, context/permission handling, per-command and aggregate
read errors, directory/cardinality/visibility proofs, unchanged copy outcomes,
lease/cancellation/reclamation behavior, and source/evidence boundaries. The
ordinary scanner has no net change; small jobs retain their original calls.
No remaining actionable finding is identified. This is an agent review, not a
formal human approval. Final hosted checks and actual repository merge rules
remain separate gates.

Owned cleanup is complete and verified: **one fixture container, one network,
zero volumes**, plus the clean temporary mutable-reference checkout. All
versioned libraries, rejected implementations' commit history and private raw
operational evidence are retained. Unrelated resources, shared caches and
supplied license files are untouched. No endpoint, credential, physical identity,
operational log or local JUnit hostname is exported in the committed numeric
artifacts. Local evidence links, JSON/gzip integrity and privacy sentinels pass.
