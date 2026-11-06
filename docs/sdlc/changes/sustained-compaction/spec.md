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
