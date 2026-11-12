# Contract

Date: 2026-11-12. Base: a8de1c3c. Status: implemented locally.
See [intent](intent.md), [plan](plan.md), and
[pull request #394](https://github.com/redis/redis-adbc-driver/pull/394).

- Adoption accepts `Mode: "managed"` (also the empty default) or `"unmanaged"`.
  The CLI exposes `-mode`. Unmanaged adoption requires a compatible existing
  application HASH Search index; it never creates or owns a source index.
- `adbc.redis.foreign_indexes_mode` selects the mode for newly registered
  foreign indexes. Existing tables cannot silently change ownership mode.
- An unmanaged descriptor pins schema and source identity in the catalog, but
  owns no data generations. Reads use live source HASHes and index membership.
  Source schema/index changes require explicit re-registration/refresh.
- INSERT requires an explicit `__key`; UPDATE and DELETE affect the source.
  Source encodings and unrelated HASH fields are preserved. External writes,
  expiry, and SQL writes do not participate in managed publication. Multi-row
  writes can partially succeed; uncertain writes are not replayed.
- DROP unregisters an unmanaged table, preserving source keys and indexes.
  ALTER, TRUNCATE, SQL index changes, Arrow ingestion, retained write preparation,
  shared read snapshots, native row-ID reads, and live partition reads reject
  unmanaged tables. Mixed managed/unmanaged reads have no collection snapshot
  guarantee for the unmanaged part. Nested data-changing statements cannot
  mix unmanaged writes with managed publication.
- Source names/keys overlapping driver control or managed row/index storage
  are rejected. Managed GC, compaction and reference tracking never own external
  rows/indexes. SQL RBAC does not substitute for native source ACLs.

Acceptance: verify live reads and source INSERT/UPDATE/DELETE for both entry
paths, managed default isolation, key/encoding preservation, mode conflicts,
unsupported-operation refusal, and source survival after DROP and sweep.
Use Redis Software with verified active 4x QPF for integration tests.
