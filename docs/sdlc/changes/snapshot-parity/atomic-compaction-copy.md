# Atomic compaction copy batches

Date: 2026-11-05. Parent: 99423e7. Status: implementation and focused recovery
checks; full snapshot race validation passed; native validation passed; performance measurement pending.
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
those capabilities. Native execution and comparative timing follow sequentially
on the same exclusively owned fixture.

The versioned native library passes all 113 selected Arrow interoperability,
follow-up layout, statistics and partition tests, no skips, 25.81 s. The native
run uses Python 3.14.10, PyArrow 26.0.1 and ADBC manager 1.13.0 after the Go database
process exited; the owned database was reset and policy/TLS reverified first.
The only later Go-source change groups test imports; production sources and the
native library are unchanged. Performance remains unmeasured for this change.
