# Remaining durable import resume work

Status: design, not a shipped API. The [parity specification](spec.md) remains
the acceptance boundary. Private capture journals and atomic output consumption
are prerequisites, not evidence that an interrupted operation can resume today.

## Recovery contract

An explicit resume must reuse the exact completed capture. It must never restart
an application SCAN, reread external HASHes, reevaluate SQL/defaults or consume
Arrow input again. Incomplete capture is abortable/reclaimable, not resumable:
a Redis SCAN cursor is not a durable input checkpoint. Resuming a failed statement
must be an explicit client action; a background connection must not publish it
after the client has received cancellation or failure.

The operation needs a durable target/schema/index-policy plan and an input-complete
transition after source validation. It must preserve the metadata dependencies
needed to reject same-target changes while rebasing unrelated concurrent writes.
Private output identities, provenance and the final index layout must remain
stable across owner takeover. Successful row batches atomically create registered
output images, consume their capture inputs and record the acknowledged outcome.
The remaining capture directory identifies unfinished input without replaying
already completed output. An explicit incomplete runtime-failure marker is
abort-only; it cannot admit further output, sealing or publication.

## Ownership and publication

Current ordinary staging work expires with its writer pin and is reclaimed.
Durable jobs therefore need their own bounded retention deadline, exclusive
renewable owner lease and source/base/output references. Every capture/output/
index/phase mutation must check the current owner token. A stale process must
fail after takeover, even when it still has a live socket or an old context.
Lease release must not release job references; explicit abort, expiry or confirmed
commit retires them through the existing ownership graph.

Persisted metadata construction and final index sealing need durable phase
checkpoints too. Restore the exact metadata read/change dependencies and index
layout, rather than relying on process-local resolver caches. A prepared index
must be inspected/validated before continuing; uncertain distributed Search
commands must not be resent. Publication still has one catalog linearization
point and a separately queryable durable outcome. A lost publication reply must
not turn a committed operation into another import.

New ownership states that old collectors do not understand require a storage
protocol revision. Do not add a durable job and merely assume older protocol-14
clients will protect it. There is no upgrade path; preserve explicit format
rejection and the existing autocommit, Active-Active and noeviction boundaries.

## Required proof before exposing resume

- Stop a client after complete capture, after several output commits, during
  index preparation and after publication with its reply lost. A separate process
  must resume only the unfinished phase and return the original commit outcome.
- Mutate/delete every external source after capture. Resumed results must retain
  the captured schema, values, NULLs and original source-key bytes.
- Exercise lease expiry, stale writers, competing resumers, cancellation,
  explicit abort, retention expiry and reference reclamation with pinned readers.
- Verify an incomplete capture or runtime-failure marker cannot resume/publish;
  retained private objects remain discoverable and collectible.
- Check same-target changes conflict and unrelated publications survive rebase.
  Include temporary/schema/rekey ownership constraints rather than bypassing them.
- Repeat on Cluster API connections, primary promotion and fresh AOF/RDB recovery
  under verified Software 4x QPF. Native client handles/errors must expose an
  unambiguous operation outcome without endpoint or credential material.
- Measure capture, continuation, final sealing/publication and total retained
  storage; compare unchanged imports and long-running workloads as well as short
  successful samples. Full operation and compaction throughput remain obligations.
