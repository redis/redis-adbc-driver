# Contract and boundaries

Date: 2026-11-20. Source: `52b03c6f`. Requestor: alberttwong. Author: Codex.
Status: implementation and verification in progress; no human approval recorded.

See [intent](intent.md), [implementation plan](plan.md), and
[verification](verification.md).

- Admit at most four concurrent streamed appends per existing managed table.
  The leased server pin carries target membership; no process-local semaphore
  authorizes concurrency. Bypass the optional local exclusive writer queue for
  these appends. All SQL writers and catalog writers continue to exclude them.
- Reserve one segment slot per admitted stream: current segment count plus live
  peers must leave space below the eight-segment limit. Full capacity waits
  before input, releasing provisional pins so background compaction can publish.
  Without a maintenance worker, capacity remains explicit cancellable backpressure.
  The existing 16-manifest and 128-pin limits still apply; unrelated work or
  retained readers may exhaust manifest capacity during publication.
- Allocate row-ID ranges with an exact decimal CAS over live peer pins. Keep
  every active peer's high-water mark current, including across one peer's
  failure, completion or release. New peers seed from a fresh committed counter.
  Successful rows have unique IDs. IDs increase within each stream and are
  contiguous within each converted slice; streams can interleave slices, and
  publication order is independent of ID order. Failures may leave gaps. IDs
  allocated only by an entirely aborted cohort need not remain reserved forever.
  Keep the Flex exact-ID ceiling at 2^53 and standard signed-int64 overflow checks.
- Stage bounded row images and seal one immutable index per stream with existing
  ownership, complete-image, cardinality and readiness proofs. No private rows
  become visible until successful atomic publication.
- When the root changed, validate ordinary metadata dependencies, then combine
  the latest target's segments with this stream's final segment and retain the
  maximum row counter. Validate the complete table definition/identity and merge
  conservative TAG/NaN facts. Never replay Arrow, defaults or row writes. Retry
  only a definite metadata CAS refusal, bounded by the existing eight attempts.
- A merged append publishes its metadata construction manifest in place. Keep
  the original workspace's committed outcome separately, including in its live
  owner pin while resolving a lost reply. Do not move only the metadata map:
  native-value collection must not shrink a pinned root's native-key directory.
  Publish compaction eligibility against the actual committed root identity.
- Cancellation, stream errors, lost allocation replies and expired ownership abort
  the affected private output; completed peers survive. Unknown row-batch and
  publication replies use the existing durable outcome rules. Uncertain allocations
  are never retried. Reclamation respects reader pins and data reference edges.
- Background workers inherit Flex capability so plain-index descriptors remain
  readable through automatic compaction. Retained prepared-operation ingestion
  continues to use exclusive admission; its serialized plan is unchanged.

Compatibility: catalog format 17 and native row layouts are unchanged. New pin
fields are transient. Older cooperating admission clients observe ingest_target
and stay exclusive; they do not gain parallel streams. As before, writers that
predate the participating table/catalog admission protocol are outside the
concurrency guarantee. No new public option or table ownership mode is introduced.

Acceptance requires exact rows, values, IDs, affected counts, snapshot isolation,
independent processes, competing publication/failure/cancellation, capacity and
reclamation checks on Software with verified active 4x QPF, including actual Flex.
Measure a synthetic serial/parallel comparison with CPU, memory, client-side
admission/publication durations and separately attributed compaction; publish
only observed results with environment and scale limitations.
