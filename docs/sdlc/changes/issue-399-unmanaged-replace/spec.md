# Specification

2026-11-17. Source `b63a010d`. Status: implemented; locally verified in the
dedicated Software configuration recorded in [validation](validation.md).
Request: [intent](intent.md). Execution: [plan](plan.md). Checks: [validation](validation.md).

- Extend unmanaged_source JSON with exact schema/table consent and mandatory
  exclusive_quiescent, discard_ttls and reset_index booleans. Keep ordinary
  replace refused without that object; refuse consent in other modes.
- Accept only an existing permanent non-foreign dedicated one-prefix unfiltered
  HASH registration. Refuse overlapping/unreadable indexes, another registered
  consumer, unmodeled/aliased index attributes, nested/non-HASH/unsafe keys,
  missing string __key, retained/shared/staged contexts and managed targets before
  source mutation or Arrow Next.
- Inventory SCAN results independently of Search; retain deduplicated DUMP and
  absolute-expiry observations sorted by key, capped at 100,000 / 64 MiB. Never
  persist SCAN cursors. Validate actual-principal permissions before deletion.
- Delete observed old images individually with atomic type/DUMP/expiry guards,
  then require an empty prefix. Never delete a new interloper through a rescan.
  Recheck current registration and exact index-definition fields before mutation.
- Drop only the configured index without DD. Create a membership TAG NOINDEX
  index with the new first public field; explicitly discard prior index options.
  Publish only registration metadata using the existing guarded catalog path,
  requiring the old descriptor to match. Data has no publication step.
- Reuse unmanaged Arrow INSERT conversion/pipelines and lower-bound progress.
  Track input keys with explicit caps; check dedicated scope/duplicates and fresh
  registration/index identity per pipeline. Independently verify final key set,
  HASH type and persistent TTLs under the inventory cap.
- On failure preserve status/SQLSTATE and return separate acknowledged deletes,
  acknowledged rows, phase and conservative latest mutation outcome. Never replay
  source mutations, DDL, consumed input or uncertain publication.
- Require all writers/DDL quiescent administratively. No source locking, atomic
  cross-system transition or arbitrary raw-writer conflict-detection guarantee.
  DROP/GC retains external resources after both success and failure.

Initial/final inventory overflow, partial index/catalog transitions, expiry and
late conversion/reader errors are explicit failures, with operator-owned recovery.
There is no storage protocol version change. Catalog publication remains managed
metadata control; source data never enters managed row publication/reclamation.
