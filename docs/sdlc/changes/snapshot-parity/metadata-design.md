# Metadata/publication implementation direction

Status: steps 1 and 6 verified in prior checkpoints. Step 2, removal of legacy
name registries, and independent metadata-object reclamation are implemented;
full validation is in progress. Incremental row references are implemented
locally in format 9, replacing publication enumeration. Format 10 adds locally
verified independent publication; bounded bucket
construction and large-operation work remain open.
Parent implementation: b4b73e0.
See [plan](plan.md). This is a work record, not a completed parity claim.

The original workspace copied every metadata key and enumerated tables for
validation/reference publication. Those paths now use shared metadata, dirty-table
validation and descriptor-owned references. Unrelated writers now use metadata-only rebasing after a base-root CAS refusal,
while changed dependencies still conflict. The immutable-row design must remain intact while
removing those costs.

Proposed implementation:

1. Validate the root's metadata structure when pinning; validate only actual row
   sources lazily, once per immutable index per statement. Catalog enumeration
   remains explicit for SQL catalog queries and maintenance. Direct row-ID reads
   must still fail closed when a directory references a missing HASH.
2. Replace full workspace copying with a persistent fixed-width bucket map from
   logical metadata keys to immutable native Redis values. Copy only touched
   buckets/values. Keep native set/hash semantics for DDL collections; ordinary
   DML should change only its target descriptor and sequence.
3. Snapshot-owned UUID names do not need legacy global prefix/index reservation
   sets. Replace legacy moved-prefix tests with snapshot pin/ownership checks;
   keep the legacy paths for their existing internal/storage tests. Snapshot
   data references and reclamation, rather than those sets, own storage lifetime.
4. Reference-count shared metadata buckets, native-value owners and row data.
   Root retirement decrements bucket references in bounded durable GC steps;
   zero-reference buckets release their metadata/data edges. Staging and durable
   compaction retain unpublished outputs independently. Never keep an old root's
   entire catalog/data reference list alive merely because one metadata value is
   shared. Keep graph counters separate from bounded lease/control enumeration.
5. Track metadata read/write sets. Before publishing after an unrelated commit,
   compare the recorded read versions, merge only dirty keys into the newest
   root's changed buckets, then CAS that root. Rebase metadata only; never replay
   SQL, parameters, volatile expressions or Arrow input. Changes to a read/target
   table still conflict. Original and merge-base pins protect both trees until
   the durable publication outcome is known.
6. Defer an ordinary private segment's index until its rows are sealed; private
   reads use authoritative directories. Build the final immutable index once.
   Keep the separate compaction copy/index/install phase fences. No-op UPDATE/
   DELETE should avoid publishing a new empty segment/index.
7. Large operations require bounded staging batches with renewable ownership and
   one final publication, plus controlled admission. Compaction should schedule
   dirty lineages without repeatedly scanning an unrelated catalog and must keep
   pace under sustained-write tests. Do not address these by raising constants
   without memory, interruption, recovery and throughput evidence.

Acceptance must include independent concurrent table writes, same-table/read-set
conflicts, old pins and live partition sets across metadata GC, temporary/external
copy references, exact counters, late writers, uncertain publication replies,
failover/AOF recovery, many-table benchmarks and full feature regression checks.
