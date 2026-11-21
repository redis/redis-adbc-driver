# Immutable generations across shards: coordination assessment

Date: 2026-11-20. Base: `bc6780e9`. Status: proposed concrete contract; reference
model and runtime proof pending. [Intent](intent.md) · [Spec](spec.md) · [Plan](plan.md)

## Revised conclusion and code evidence

Immutable managed partitions can be written on several shards and published as
one logical table by a single-slot manifest decision. Every reader pins that
manifest and reads only its immutable partition identities. This construction
does not require a coordinated live-shard read view. The earlier blanket dependency
on such an upstream facility was overconservative. #339/#340 remain useful for
native/live read views; their unavailable APIs do not prohibit this work.

The existing implementation already separates central decisions from much of
physical cleanup. [Publication](../../../../go/snapshot.go) checks the current root,
workspace and references, then changes the root and terminal outcome centrally.
[Data references](../../../../go/snapshot_data_refs.go) require live exact physical
identity before retaining a segment; releases enqueue zero-reference candidates.
`claim_data` changes an unreferenced generation to deleting before
`reapSnapshotStep` deletes registered rows. [Row GC](../../../../go/snapshot_gc_batch.go)
fences each bounded deletion. These are useful patterns; moving their checks to
different slots requires new participant records and proofs, not prefix edits.

Current [row layout](../../../../go/snapshot_rows.go) and index/directory/write
fences explicitly use `{meta}` and protocol 17. That is the present implementation,
not a fundamental requirement that immutable row payloads share the catalog slot.

## Placement and local preparation

A new-format central plan names a fixed bounded set of participants. Each entry
binds table identity, schema version/hash, generation/incarnation, physical prefix,
placement tag and partition identity. Register the plan and preparation references
before input consumption or any remote initialization. Deterministically partition
logical row IDs; preserve original hidden provenance independently of physical IDs.

Co-locate each participant's HASHes, exact directory, ownership fence, batch
outcome and seal in its own tag. A bounded worker pool batches by participant;
cap aggregate queued bytes and apply backpressure to Arrow consumption. Several
tags may map to one primary, so topology/command counters must establish actual
distribution. Changing physical slot ownership is distinct from changing the
logical partition map; migration/failover behavior needs native validation.

Local batches atomically validate exact identity, owner/fence, expected checkpoint
and permissions, then register complete images and their durable outcome. Unknown
replies are resolved by reading that outcome; no caller input replay. A local
writable-to-sealed transition fixes row membership, values, counts and index
readiness. Seal receipts bind the exact central plan and participant incarnation.
A seal never expires merely because a writer lease ends. Subsequent writes fail.
The source need not be replayable after process loss; partial private work can abort.

## One authoritative central publication and retention decision

Keep all commit/abort decisions, manifests, preparation/job references, reader
pins and authoritative transitive liveness in one central slot. Central Lua/CAS
publishes only a complete verified seal set with unchanged schema/dependencies;
it atomically installs the manifest, transfers preparation ownership to manifest
ownership and records the terminal outcome. An uncertain response reads this
unique durable outcome. A conflicting publication does not replay SQL/Arrow.
A canceled writer cannot delete an already committed generation.

Pin acquisition is atomic with selecting the central committed manifest. It
protects that manifest's transitive participant references. Participants do not
independently expire committed/prepared data or decide liveness from local lease
age. Therefore independently fetching their already sealed images still reads
one logical table snapshot. Native live read-view acquisition is unnecessary.
Expired/invalid reader pins fail under the normal bounded reader contract.

Every partition and query path must verify the expected immutable incarnation,
seal and exact directory; missing/replaced participants cause an explicit error.
Never interpret absence as an empty partition or reopen a live generation.
Logical version/tombstone resolution precedes predicates and aggregation; global
merge precedes DISTINCT, ORDER BY, OFFSET and LIMIT. Search indexes must be sealed
and ready for the exact partition or reads use the exact immutable directory.
Existing local Search/routing assumptions need implementation work. All canonical
queries require complete, independently checked results before a correctness claim.

## Terminal reclamation authorization: resolving the GC race

A receipt followed by a root CAS alone does not prevent premature collection.
The missing rule is monotonic central retirement:

1. A central atomic transition proves zero transitive reader/manifest/writer/job
   references, removes future publication/reference eligibility for that generation
   permanently, and records an exact-identity reclamation grant.
2. Publish/retain and retire inspect the same central state. If publication wins,
   retirement is forbidden by its references. If retirement wins, late seal reports,
   publication or reference acquisition fail permanently. An abort/expiry decision
   must first make the writer's outcome terminal; local clocks do not authorize GC.
3. A collector delivers the grant, possibly much later or more than once, only
   for its bound generation/incarnation/prefix. The local script atomically enters
   deleting before any physical deletion, rejecting subsequent row writes/seals.
   A grant arriving before delayed local initialization must also establish that
   terminal fence, so a late initializer cannot recreate the output.
4. Delete registered rows/indexes/directories in bounded guarded steps, retain
   progress/outcomes, then acknowledge completion centrally. Unknown completion
   replies require reconciliation. Do not erase terminal evidence while old work
   can arrive; safe tombstone truncation/epoch rotation is a separate proof.

No cross-shard atomic command is required for those ordering rules. Remote deletion
is asynchronous after an irreversible central decision. Network loss causes retained
private data/backpressure, rather than speculative deletion. Registered unique
identities and terminal fences prevent late-writer resurrection. This is a driver
coordination protocol to implement and verify using current primitives, not a
claim that Redis supplies distributed transactions.

## Failure model and the actual durability gate

The positive reference model must state that committed central and participant
history does not independently roll back. It can then explore delayed/lost replies,
writer crashes, cancellation, partial preparation, lease expiry, reader pins and
reordered/duplicated cleanup deliveries without requiring live views.

Rollback is a real separate counterexample: central retirement issues grant G;
a remote shard deletes; the central shard loses that retirement and recovers an
older root referencing the deleted participant. Likewise a surviving catalog
commit can reference a participant whose sealed writes were lost. Ordinary local
reference checks do not solve either case. Split-brain/stale authority and reset
or restore epochs must also be fenced; a client clock is insufficient.

[Software replication consistency](https://redis.io/docs/latest/operate/rs/databases/durability-ha/consistency/)
documents asynchronous acknowledgment and WAIT/WAITAOF alternatives. The
[Software WAIT guide](https://redis.io/docs/latest/operate/rs/clusters/optimize/wait/)
and [WAITAOF command](https://redis.io/docs/latest/commands/waitaof/) describe
connection-scoped prior-write acknowledgment and returned counts, including timeout.
Those facilities are candidates for durable-before-publish and durable-before-delete
barriers. They are not, by themselves, a proof against history rollback or every
possible promoted member. Do not issue a barrier on an unrelated pooled connection
and infer it covers earlier writes; verify routing and connection affinity.

[WAIT](https://redis.io/docs/latest/commands/wait/) explicitly does not establish a
strongly consistent store; WAITAOF likewise retains failover/restart qualifications.
[Software persistence](https://redis.io/docs/latest/operate/rs/databases/configure/database-persistence/)
documents AOF policies and that Flex/Auto Tiering flash is not persistent storage.
Runtime support must bind exact product/version, AOF configuration, acknowledged
counts, all eligible failover copies and recovery epochs. Verify the complete
protocol under promotion/restart/restore; do not infer a guarantee from an open
feature request, replica count alone or a successful small command probe.

Until that failure contract is demonstrated, the executable model and isolated
private-write prototype remain useful progress. A runtime path must fail closed
on unproven/missing participant state or topology/recovery epochs; this is not an
availability or data-loss guarantee. No sharded production safety is claimed by
the model's no-rollback assumption.

## Redis primitive boundaries and backup

[Multi-key command guidance](https://redis.io/docs/latest/develop/using-commands/multi-key-operations/)
and [Software resharding guidance](https://redis.io/faq/doc/2aur5jkvx8/what-needs-to-be-checked-before-resharding-a-redis-enterprise-database)
require attention to slot locality. Proxy handling of some variadic commands does
not make arbitrary multi-slot Lua/MULTI atomic. Keep each local batch wholly local
and the central decision wholly central.

An immutable manifest also supplies a possible logical backup boundary: pin it,
retain all referenced sealed participants through export, copy them, and verify
complete exact identity/count coverage before finalizing a backup manifest.
Copies need not occur simultaneously because those objects do not change.
This requires a deliberately specified backup format and restore/recovery epoch;
it does not establish compatibility for arbitrary independent shard RDBs or
silently authorize protocol-17 backup reinterpretation. Partial snapshots, missing
participants and stale writer/delete capabilities must be rejected on restore.

## Next reviewable step and remaining scope

Implement a bounded executable reference model of the central authority, two
participants, preparation/seal/publication, one reader and delayed terminal GC.
State explored bounds; assert no partial publication, no pinned reclamation, no
post-retirement reference/publication, no local resurrection and no mutation/input
replay. Include rollback as an explicit failing-assumption trace. Model result is
pending in this documentation commit; even a passing model is not a Go/Lua or
multi-primary durability proof.

Then add a new-format experimental participant layout and narrow bounded private
batch/seal operations before ordinary SQL/Arrow publication/GC integration.
Real multi-primary Software/Flex QPF4 tests, schema/overlay/query/backup correctness,
CPU/storage attribution and end-to-end throughput remain issue acceptance work.
The user's continued #568 request authorizes continuing that scoped engineering
work; the earlier generated document's approval wording is not an approval policy.
