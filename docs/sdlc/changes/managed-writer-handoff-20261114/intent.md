# Managed writer handoff: intent

Date: 2026-11-14. Requestor: repository user, asking for the next performance tasks with subagents. Baseline: `d7c0978f` (merged PR #440). Status: measured candidate; Go throughput/tail tradeoff observed, pending native confirmation and maintainer review.

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
