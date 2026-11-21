# Multi-shard managed ingestion: specification

Date: 2026-11-20. Base: `bc6780e9`. Status: proposed contract; model and runtime
proof pending. [Intent](intent.md) · [Design](design.md) · [Plan](plan.md)

## Consistency and ownership contract

1. One central slot owns logical schema, immutable manifests, participant plans,
   commit/abort outcomes, reader pins and the authoritative reference graph.
   A single central CAS publishes one complete manifest. No cross-slot Lua or
   MULTI atomicity is assumed.
2. Register exact participant generation/incarnation, prefix, placement tag,
   schema identity and owner before any local writes. Bound partitions, workers,
   queued bytes, batch rows and batch bytes. Record actual physical distribution
   separately from slots and client count.
3. A participant atomically registers complete row images plus its local durable
   batch outcome under its exact prefix/token/checkpoint fence. An irreversible
   seal prohibits further row/directory mutation. Sealed data do not expire or
   self-delete when a writer/reader lease expires.
4. Publish only an exact planned set of verified sealed participants and pinned
   dependencies. Transfer preparation references to committed manifest references
   in the same central decision. Resolve unknown decisions through their durable
   outcome; never resend unknown input or mutations.
5. Central pins retain manifests and their transitive participant references.
   Immutable sealed data from one pinned manifest establish a logical snapshot
   without independently acquiring live shard views. Missing/changed participants
   cause explicit failure, never omission or substitution of live rows.
6. Reclamation is authorized only by a central atomic terminal transition after
   every reader/manifest/writer/job reference is absent. It permanently prevents
   new references/publication for that generation before emitting a durable
   exact-identity deletion grant. A delayed grant remains safe to deliver.
7. A participant consumes that grant by atomically fencing itself as deleting,
   including when local preparation has not arrived yet, before bounded physical
   cleanup. Late writers/initializers cannot resurrect it. Keep terminal evidence;
   safe evidence truncation/epoch rotation is separate work.
8. The model's successful safety proof assumes committed central and participant
   history does not roll back independently. Model central-retirement rollback
   after remote deletion as a negative counterexample. A supported durable/failover
   contract or explicitly validated fail-closed recovery is required for runtime
   claims; WAIT/WAITAOF success alone is not a strong-consistency proof.
9. Preserve logical row IDs/provenance, exact layered visibility, global aggregate,
   ordering and limit semantics. A new placement/storage format needs explicit
   protocol/capability fencing; protocol-17 backups are never silently reinterpreted.

## Acceptance status

This revision delivers a concrete contract and corrects the native-view dependency.
Executable model results, its actual explored bounds, per-primary Software/Flex
QPF4 runtime tests, failure/durability/restore tests and all 43 canonical query
results remain pending. They are not established by a reference model or by this
record. Continued model/design implementation is within the user's existing
request; no additional approval is inferred from earlier generated wording.
