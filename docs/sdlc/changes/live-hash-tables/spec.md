# Specification

Date: 2026-11-18. Status: implemented locally; focused verification passed.
See [intent](intent.md) and [plan](plan.md).

- An explicitly enabled `redis_live` schema exposes registered application HASH
  collections. The application owns the keys and their values; the driver owns
  only its registered SQL definitions and generated Search indexes.
- SQL SELECT, INSERT, UPDATE and DELETE operate on the original HASHes. INSERT
  generates `<registered-prefix><uuid>` through the existing volatile SQL
  `GEN_RANDOM_UUID()` default when `__key` is omitted or DEFAULT. Explicit keys
  are preserved; explicit NULL and collisions are refused. Generated keys are
  returned by `RETURNING __key`; no internal fields are added to the HASH.
  UPDATE preserves unknown
  HASH fields and TTLs, does not recreate vanished rows, and refuses an update
  that removes the last field. DELETE removes the selected source keys.
- SQL writes use per-row Redis operations, not managed statement snapshots.
  Multirow errors can leave earlier rows changed. Concurrent read-modify-write
  updates can lose intervening changes; no transaction or snapshot is implied.
  Selective index candidate discovery follows server indexing visibility;
  full HASH scans read the application keys directly. Direct key reads
  read the current HASH. Indexing failures must be surfaced rather than treated
  as complete query results.
- Registration scans existing rows to establish a fixed SQL schema and builds a
  driver-owned index in place. Subsequent rows under the registered prefix are
  visible without an import or refresh. As in existing key-row collection rules,
  keys with another colon after the prefix belong to nested collections.
- Direct Redis writes must respect the registered field encodings. New fields
  are not automatically added to the SQL schema. Direct changes to driver-owned
  indexes or metadata are unsupported.
- Removing a registration removes its own index without `DD`, and never deletes
  source HASHes. Managed reclamation must never traverse a live source prefix.
- Shared managed read snapshots, retained execution, Arrow ingestion,
  schema-changing DDL, TRUNCATE and mixed managed/live SQL writes are excluded
  from the initial implementation. Normal live reads may join managed reads,
  but the live portion has no snapshot consistency.
- Live scans preserve exact binary row keys and recheck the full predicate on
  fetched values. Bounded SCAN/INKEYS batches avoid Search aggregate key
  truncation and deep Search result caps. Initial collection scans materialize
  SELECT results; index ordering/reducers are not used on mutable rows. The
  prefix index can include nested HASHes, which must have compatible indexed
  encodings even though they are excluded from SQL rows.
- Registration/index lifecycle and live metadata use a separate control
  namespace, outside managed row ownership and compaction. Unknown mutation
  outcomes must not cause automatic replay or source deletion.

Acceptance covers bidirectional SQL/Redis visibility, insert collisions, unknown
field and TTL preservation, missing/invalid values, nested keys, source survival
after unregister/managed sweep, rejection of excluded operations, and unchanged
managed snapshot behavior. Server checks require Redis Software with verified
active 4x QPF. No performance improvement is claimed without measurement.

Pull request: [#510](https://github.com/redis/redis-adbc-driver/pull/510).

Documentation must compare all three ownership combinations consistently,
separate index ownership from server-side storage and automatic index-entry
maintenance, and keep source-build requirements and per-mode write guarantees
explicit. The README should provide an end-to-end SQL/Redis example with
generated keys, without implying that installing a published wheel enables
unreleased source features.
