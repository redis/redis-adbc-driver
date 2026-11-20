# Implementation plan

Date: 2026-11-20. Initial investigation: `52b03c6f`; implementation rebased onto
`845c95b4`. Requestor: alberttwong. Author: Codex.
Status: implemented and locally verified; human review and hosted CI pending.
No human approval or release is recorded.

See [intent](intent.md), [specification](spec.md), and
[verification](verification.md).

1. Extend native admission with four leased append peers and segment reservations;
   preserve exclusive SQL/catalog gates and bypass local exclusive scheduling.
2. Add exact fenced peer row-ID allocation and private counter recording.
3. Merge completed append descriptors/counters over current metadata without
   replay; preserve native metadata directories, outcomes and compaction identity.
4. Carry Flex capability into the background compactor.
5. Exercise overlapping Arrow streams, independent processes, failures and lost
   replies, metadata CAS and expired leases, capacity/compaction and private GC.
6. Run unit checks and relevant race/regression selections, then actual Software
   Flex checks. Compare controlled serial/parallel ingestion and document results.
7. Review lifecycle, ownership and evidence against REVIEW.md; update user docs.

Implementation lives in go/snapshot_append.go, snapshot_admission.go,
snapshot_publication.go, snapshot.go, ingest.go, store.go and snapshot_compaction.go.
New integration and opt-in measurement fixtures accompany these changes.

Implementation and local checks are complete. Actual commands, retained failures,
measurement limits and self-review are in [verification](verification.md). Human
review, hosted CI and release are not inferred from local verification.
