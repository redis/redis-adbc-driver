# Finite-ID partition reads without Search readiness

Date: 2026-11-05. Status: focused implementation verified; full combined and
independent restricted-role checks remain required. See [spec](spec.md),
[plan](plan.md).

The documented finite-ID partition route reads authoritative HASHes without
Search cursors, caps or readiness. Snapshot planning previously called getTable
first, which opened every source index through FT.INFO before reaching that
route. A reader denied FT.INFO could not use a direct partition.

Partition planning now establishes the bounded ID proof before loading data
metadata. The direct route validates canonical descriptors, live ownership
records with matching physical prefixes, positive canonical reference counts
and exact directory cardinalities. Binding uses this one validated table only;
the scoped override is restored afterward. A post-binding route change must
perform full index validation. Native-only evidence never populates the Search
validation cache.

Every fetched direct row, including a present HASH, must have score-zero
directory membership. Batched ZMSCORE/cardinality checks distinguish absence,
registered missing images, unregistered images and malformed scores. Newer
registered tombstones stop lookup; a rogue newer HASH cannot shadow an older
valid row. Reader pins, cancellation, Arrow ownership and final row identity
checks remain in the existing pipeline. The canonical baseline reader ACL gains
the read-only ZMSCORE command; its key families and write permissions are unchanged.
Broad reads still require FT.INFO, with a typed unauthorized error for an actual
native permission refusal from that command.

## Evidence

Tests use an owned strict-TLS Redis Software 8.8.0 fixture with noeviction and
active/effective 4x QPF, verified through its API. The fixture uses the Software
proxy; this is not Cluster API or failover evidence. Permission refusals here are
injected as direct native-shaped errors on the read client's FT.INFO commands;
they are not presented as actual Software role provisioning evidence.

- Before the fix, the finite partition fails with `published snapshot index is
  unavailable: NOPERM … ft.info` (case **0.68 s**, root **1.526 s**).
- The focused native-state selection passes **11 test events**, no skips or
  failures, root **5.694 s**. Eight corruption cases modify real owned native
  records: missing ownership, wrong prefix, zero references, wrong cardinality,
  a registered missing HASH, same-cardinality unregistered membership, nonzero
  scores and a rogue newer image. Each refuses before emitting rows, without
  falling back to Search. Fixture-owner repair precedes reclamation.
- The expanded partition/RBAC race selection passes **96 test events**, with
  **2 existing Software capability skips**, no failures/race reports, root
  **26.916 s**. Its direct read checks exact BIGINTs, NULL and tombstone
  visibility across DROP/GC, deliberately missing Search indexes and a denied
  FT.INFO command. Broad reads retain the permission failure. A separate test
  proves that native validation cannot authorize a subsequent index scan on the
  same pin. Existing partition parameters, ranges, lifecycle and layouts remain
  included. The two skips are `TestPartitionReaderPermissions` (protocol ACL)
  and `TestPartitionRuntimeCapDiscovery` (native CONFIG capability).

Actual restricted Software roles, the independent TLS native worker, full
current-source suites and current dependency/platform matrices remain separate
gates. This checkpoint does not establish throughput parity, public operation/
input resume, or multi-shard recovery.
