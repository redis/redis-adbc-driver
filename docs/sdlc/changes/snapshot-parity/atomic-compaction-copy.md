# Atomic compaction copy batches

Date: 2026-11-05. Parent: 99423e7. Status: implementation and focused recovery
checks; full snapshot race validation passed; native validation and fresh comparisons passed; sustained performance parity still fails.
See [intent](intent.md), [spec](spec.md), [plan](plan.md), and the
[directory-index comparison](directory-indexing.md).

The preceding comparison improved ordinary point-update latency but failed the
sustained-write gate: compaction/admission still dominated tail latency and
throughput. This change replaces one fenced row mutation per copied image plus
a separate checkpoint with one bounded atomic copy command. It preserves the
64 merged-ID limit, 1 MiB write target and 64 MiB single-image ceiling. Source
fetches remain bounded by row count; this does not establish a 1 MiB read-memory
bound, remove full-prefix copying or optimize compaction index construction.

The script checks current job/lease ownership, phase, exact source checkpoint,
live output ownership and physical prefix. Before mutations it validates every
canonical int64 row ID, ordering, output type/registration, complete image and
write permission. IDs remain strings. It encodes a pending outcome before
registering directories and writing images, then commits the completed outcome,
source checkpoint and renewed work lease together. Zero-live-image pages still
advance over deleted source IDs. Unexpected runtime failures leave registered
partial output and a pending marker; no metadata checkpoint may clear that
marker or enter indexing. A replacement worker repairs images from the same
retained immutable source. A checkpoint cannot rewind completed progress, and
error logging updates only the current durable error field.

An unknown reply triggers a bounded read of durable progress, never a resend.
Typed server refusals remain failures, including stale owners whose successors
already advanced. Optional job outcomes do not introduce a new storage format.
Public SQL plans and unread Arrow input remain outside this recovery contract.
No segment/admission limit is raised and no caller SQL is retried.

Focused evidence before the final incomplete-marker guard: 18 passing race test
events, zero failures/skips, root 6.083 s, including the existing failed-compaction
publication regression adapted to inject an actual partial Lua batch. Earlier
compaction/resource/recovery selection: 22 passing events, root 49.671 s. These
runs used owned Redis Software 8.8.0, strict TLS, verified active/effective 4x QPF,
noeviction and three shards behind the Software proxy; they are not Cluster API,
HA or persistence-restart evidence. The initial focused invocation failed shell
parsing before tests; its quoted selection was corrected before the passing run.

Retained faults cover lost successful replies, unavailable outcome reads, stale
owners, late-row preflight rejection, all-deleted pages with old readers, a real
runtime failure after the first written image, replacement-worker repair,
reference-safe GC and stale metadata checkpoints. The stale-checkpoint test
reproduced rewind before the monotonic fence. The original batch test reproduced
per-row command overhead and unresolved committed replies before implementation.

Author review covers lifecycle/checkpoint monotonicity, partial-image retention,
exact namespace/ownership preflight and non-replayable unknown outcomes. Full
snapshot/native checks, fresh both-order benchmarks, actual narrow-principal ACL
coverage, current Cluster API/HA/persistence checks, public operation resume and
complete feature/performance parity remain open. No throughput improvement is
claimed from command-count reduction alone.

The final production source passes unit-only race verification: 1,037 passing
individual test events, 933 expected integration/diagnostic skips, zero failures;
root package 52.052 s. The versioned native build succeeds; SHA-256
`1ed71a0bed5b5f64ab5aaf2b6c546d158e1af74d0ca68feb91782cca5cc2f219`.
Go 1.27.3/macOS ARM64. This evidence does not replace database/native execution.

The complete snapshot race selection after the incomplete-marker guard passes
370 individual test events, zero failures and 23 explicit skips; root 273.251 s.
Skips comprise nine absent Cluster fixture cases, six reader-lease model cases,
six permission/recovery cases and two opt-in profiling cases. They do not prove
those capabilities. Native execution and comparative timing below follow sequentially
on the same exclusively owned fixture.

The versioned native library passes all 113 selected Arrow interoperability,
follow-up layout, statistics and partition tests, no skips, 25.81 s. The native
run uses Python 3.14.10, PyArrow 26.0.1 and ADBC manager 1.13.0 after the Go database
process exited; the owned database was reset and policy/TLS reverified first.
The only later Go-source change groups test imports; production sources and the
native library are unchanged. Fresh performance evidence follows below.

## Fresh sustained-write comparison

[Raw samples and complete run metadata](atomic-compaction-comparison.json) compare
`8d9f02b` (directory indexing, previous per-row compaction), `534cd6f` (atomic
compaction copies) and mutable `f4d5b78`. Order: previous, atomic, mutable, mutable,
atomic, previous. Each process constructs two 20,000-row tables, performs one
warmup update per table and 64 alternating point updates with RETURNING, checks
every returned exact BIGINT, then fetches and compares all 40,000 final rows and
all three columns. There are no caller retries, concurrent database observers,
builds or tests during measured intervals. Each revision contributes 128 timed
operations across two processes; setup and final independent verification are
outside the interval. All six processes terminate successfully and validate every
result. This is a finite synthetic workload, not ClickBench.

| Build | Median update ms | p95 ms | Maximum ms | Updates/s |
|---|---:|---:|---:|---:|
| Previous snapshot, 8d9f02b | 32.351 | 7,701.206 | 7,863.791 | 0.708 |
| Atomic compaction, 534cd6f | 30.463 | 6,964.266 | 7,143.579 | 0.775 |
| Mutable baseline, f4d5b78 | 5.521 | 6.630 | 34.480 | 170.490 |

Atomic compaction improves pooled throughput **1.095x** and median update latency
**1.062x** over the previous snapshot build. The mutable baseline remains
**219.95x** faster in throughput and **5.52x** faster at the median. The sustained
performance-parity gate still fails. These measurements support retaining the
recovery/command-count improvement, not a claim that compaction now keeps pace.
Next attribution should separate full-prefix copies, repeated per-step catalog/
index checks, the worker's fixed pacing and compaction initial indexing; none has
been established as the dominant remaining cause by these end-to-end samples.

All runs use the same verified Software 8.8.0 strict-TLS fixture, active/effective
4x QPF, noeviction, three primary shards behind its proxy and macOS ARM64. The
unchanged harness SHA-256 is recorded in each raw result. Benchmark runtime:
Python 3.13.16, PyArrow 26.0.0, ADBC manager 1.13.0; the separate native validation
cohort above uses Python 3.14.10/PyArrow 26.0.1. The previous and mutable libraries
were preserved and hash-verified before every invocation. The current binary hash
matches the native-validated build above.

The first complete ordinary Go race attempt stopped at
`TestSQLSnapshotCleanupDeniedRecovery` (root 1.908 s). Its automatic production
worker can legitimately reclaim the fixture between the denied and authorized
explicit collector steps, leaving zero rows for the latter's exact deletion
count. The test now opens its database with automatic maintenance disabled,
matching the explicit-collector protocol fixtures, while preserving all denial,
immediate takeover, exact three-row accounting and pinned-live-value assertions.
Ten consecutive focused race invocations pass (root 4.281 s). No production
collector behavior or permissions change. A fresh full ordinary run remains
pending; the failed attempt is not counted as a pass. The manually dispatched [hosted test matrix](https://github.com/redis/redis-adbc-driver/actions/runs/42064051718)
targets `534cd6f` and is in progress (helper/coverage jobs passed at inspection).
Neither pending run is counted as passed. Current protocol Cluster API/HA/
persistence, actual restricted-principal authorization, public operation/input
resume, broader platform acceptance and complete performance parity remain open.
