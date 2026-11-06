# Contract and acceptance

Date: 2026-11-06. Status: investigation. See [intent](intent.md), [plan](plan.md)
and [#337](https://github.com/redis/redis-adbc-driver/issues/337).

- Exercise sustained sparse replacements, mixed replacements/deletions and
  dense writes. Report complete-write throughput, median/p95/p99/max latency,
  segment/candidate backlog, compaction phases, retained native memory and
  cleanup lag. Use long enough runs to span many maintenance cycles and retain
  time windows so drift is visible.
- Separate passive controlled phase attribution from isolated native latency
  measurements. Observe storage/backlog between timed windows; record probe
  costs and avoid concurrent builds/tests/probes during timed writes. Validate
  all returned values and every final row/multiplicity independently, with no
  caller SQL/Arrow retries.
- Preserve pinned readers, exact BIGINT/NULL/provenance/tombstones, source range
  identity, newer tails/DDL, cancellation, owner takeover, durable checkpoints,
  unknown-outcome behavior and reference-safe reclamation.
- Keep the four-segment trigger, eight-segment admission limit, bounded copy
  rows/bytes and statement budgets. Do not force foreground compaction or add
  mutation replay, a consistency-off switch, or a new storage protocol.
- Evaluate supported upstream alternatives before adding driver infrastructure.
  Upstream proposals are not implemented capabilities. Record residual gaps and
  source/settings with every comparison; mutable storage provides no snapshot
  guarantee and is only a performance reference.

Closure requires source-specific measurements, any retained optimization's
regression/correctness evidence, final selected CI, delivered code/evidence and
verified owned-resource cleanup. No unmeasured universal parity claim.

The owned acceptance workload uses two 20,000-row tables for sparse and mixed
delete/reinsert writes, and two 1,024-row tables for dense writes. Four timed
windows span at least 60 seconds of offered load per process, in both source
orders. The matched comparison declares the same maximum of 40 statement starts
per second for every source; report actual delivered rates and local pacing.
This establishes behavior at that offered-load ceiling, not maximum mutable
throughput. Retain unpaced exploratory results, including the two repeated
mutable-reference connection resets, separately without labeling incomplete
processes successful. The limit defines the workload and is not a driver fix or
a causal explanation of those failures.

Sample backlog immediately at each window's end, before full-row checks
or storage scans. Record the intervening validation/probe gaps explicitly; this
is windowed sustained load, not an uninterrupted long-duration soak.

Targets are zero caller retries/refusals or incorrect results, the unchanged
eight-segment/generation admission bounds, fewer compaction read round trips,
and complete eligible-work drain observed within 30 seconds of the final write.
The drain clock includes final validation/storage sampling; its first observed
empty state is an upper bound at the stated observation cadence. Report retained
memory and per-window drift, remaining mutable throughput gaps and negative
cases. Do not turn finite workload observations into universal steady-state or
full performance-parity guarantees.
