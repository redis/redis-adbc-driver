# Managed writer handoff: intent

Date: 2026-11-14. Requestor: repository user, asking for the next performance tasks with subagents. Baseline: `d7c0978f` (merged PR #440). Status: opt-in measured candidate; separate Linux native confirmation complete, pending maintainer review.

Managed writers currently poll fresh catalog/target/server admission independently. The last target-progress pacing experiment increased command amplification and reduced throughput; it remains rejected. This experiment instead tests whether bounded, process-local FIFO turns can suppress redundant local polling and hand off eligible writes promptly. Server admission remains authoritative, including for other processes.

Success requires preserved statement/Arrow lifecycle and exact final state, lower complete-workload latency/tails or higher throughput without harmful command amplification, followed by a normal native Python/ADBC comparison before shipping any runtime candidate. No global fairness, cross-process queue, new storage protocol, cached authorization or SQL/input replay is proposed.

See [specification](spec.md) and [plan](plan.md). PR: integration pending.

## Explicit opt-in gate added after measurement

The public database option `adbc.redis.writer_admission_queue` accepts exact
`true`/`false` strings and defaults to `false`. It affects newly opened
connections; existing stores retain their selection. Default false avoids
identity fingerprinting, the extra trust-bundle read and local registry work.
True opts eligible managed writers into the bounded process-local tradeoff; it
is not another driver/table mode, a server-admission bypass or a global FIFO
guarantee. Live unmanaged writes are unaffected.

The always-on `03e04002` prototype supplied the recorded Go pilot/long timings.
Adding this public option is a functional post-measurement change. Its actual
default/enabled option, lifecycle and safety checks are recorded separately, and
a new frozen native candidate must enable/verify the public option before any
final selection claim. The preceding Go measurements must not be relabeled as
timings of the final gated build.

## Separate final gated Linux native confirmation

The [frozen Linux/arm64 Python/ADBC ABBA report](../../../../benchmarks/managed-writer-handoff/native-linux/README.md)
uses actual baseline `d7c0978f` and candidate `0884e3c8`, with the public option
explicitly enabled and GetOption-verified on every candidate driver connection.
All 192 records/6,144 operations passed complete original typed/final-state
oracles. Independent offline audits verify exact grids, counts, markers,
statistics, source/library/wrapper identities and cleanup; raw bytes are retained.
Four-writer completion throughput/p95 improve, while medians and means worsen
substantially. All slower cases and raw control drift remain visible. The option
stays default false; no general/default speedup or global FIFO is claimed.

The macOS Go prototype observations remain historical and separate. A Linux
control did not reproduce the macOS post-proof response delay, so no general
Software/driver cause is established. The native data has its own topology and
source/binary labels. Raw automatic_retries labels are unchanged; post-run notes
separate caller intent, verified zero redis-py command retry budget and uncounted
connection management/reconnect behavior. Object setup is excluded, but lazy
first-operation network/execution-pool work remains included. New clients per
trial mean warmups do not prewarm later clients. No timing code changed after
freezing. Human approval is not inferred.
