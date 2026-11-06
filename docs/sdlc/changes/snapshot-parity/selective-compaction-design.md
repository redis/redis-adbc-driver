# Selective compaction design and acceptance

Date: 2026-11-05. Status: selection implemented; complete local snapshot/native
checks and actual older-worker handoffs pass. See [implementation/evidence](selective-compaction-validation.md).
Fresh throughput improves, median latency regresses, and performance parity fails.
Complete feature/platform validation remains incomplete.
See [the phase attribution contract](compaction-phase-profile.md),
[the failed sustained gate](atomic-compaction-copy.md), [intent](intent.md),
[spec](spec.md) and [plan](plan.md).

At parent be0ed4d, jobs always merge the entire oldest prefix. A large base followed by
several one-row updates therefore causes a full-table copy when those small
updates need consolidating. Atomic batches reduce command overhead but preserve
that work. The fresh end-to-end comparison still fails performance parity.
Measure phase costs before selecting the next implementation; this document does
not attribute the measured seven-second tail to one cause.

A candidate improvement is a size-aware contiguous merge that preserves a much
larger immutable base. Consolidate the newer small segments into one immutable
overlay while retaining older segments and any deltas published after selection.
Prefer the existing full-prefix merge when its cost is comparable or consolidation
would no longer reduce segment count. Use physical directory cardinalities as
bounded estimates, with overflow-safe arithmetic; never raise the eight-segment
limit or replay admitted SQL/Arrow input to hide waiting.

## Required correctness contract

- Record the selected immutable segment identities and their starting position.
  Claiming a job must match that exact contiguous range in the durable candidate,
  validate lineage and protect every selected source/output reference. Installation
  must revalidate the same range in a pinned current descriptor, retain all older
  segments and preserve every newer delta. It cannot replace the selected range
  based on row counts or a physical UUID appearing anywhere.
- Partial-range output retains winning tombstones. Dropping a deletion would
  expose an older version in the preserved base. Only a merge covering all older
  versions can discard tombstones. The existing query, aggregate, ordered-scan,
  partition and direct-lookup visibility rules remain mandatory. Physical indexed
  rows/directory counts include tombstones; visible SQL counts do not.
- Retain bounded images/checkpoints, incomplete-batch repair, exact source IDs,
  leases, prefix ownership, warning-free readiness and unknown-outcome handling.
  A smaller logical merge cannot weaken the publication or reader fences.
- DROP/TRUNCATE/rekey, lineage replacement, concurrent tail writes, independent
  metadata edits, old pinned streams, live partitions, worker takeover and GC
  require dedicated regressions. Validate every final row and exact value, not
  just total count or the latest updated value.

## Compatibility must be demonstrated

Current storage protocol 15 represents arbitrary ordered immutable segments;
changing selection must not silently give an old worker a job it could install
as an oldest-prefix replacement. One design to test uses an optional job source
position with a Source descriptor containing only the selected range. Existing
workers compare Source against the current oldest prefix and should retire a
non-prefix job before copying/installing it. That is a hypothesis to verify with
preserved actual protocol-15 binaries, not a compatibility claim.

Retirement by an old worker must not strand a table at the hard segment limit.
A selective job would need to retain the corresponding durable candidate until
its installation updates/removes it atomically, so a worker that declines the
job can still discover eligible work. Preserve original FIFO age, conditional
candidate version checks and one active job per catalog. A replayed/unknown
claim cannot consume the only discoverable work item. Exercise takeover at copy,
index and install phases using both implementations, with reader correctness,
reference safety and eventual admission demonstrated.

No protocol-16 activation, migration, broader ACL grant or disabled snapshot fence
is part of this proposal. If the old-worker safety hypothesis fails, revise the
job design before implementation; optional JSON fields alone do not prove
compatibility. Separate index-construction changes from selection so their
recovery and performance effects can be measured independently.

## Acceptance evidence

First collect the phase profile on exclusively owned Software with verified
active/effective 4x QPF and noeviction. A selective implementation then needs
focused failing-before regressions, complete snapshot/ordinary/native checks,
actual old-binary job handling and current Cluster/HA/persistence validation.
Retain the existing large-copy resource test by making its sources genuinely
require a large merge; do not replace its assertion with a smaller copied-row
count. Repeat the six-process, both-order public native comparison with complete
RETURNING/final-row validation and no caller retries. Report remaining median,
tail and throughput gaps against both the preceding implementation and the
mutable baseline. No design-only item counts as completed parity work.
