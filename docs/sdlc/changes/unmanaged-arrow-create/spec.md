# Contract

Status: implemented and locally validated; see [validation](validation.md). See [intent](intent.md), [plan](plan.md).

- Statement option `adbc.redis.ingest.unmanaged_source` is JSON with required
  `prefix` and `index` and optional `encodings` (logical-column map, text/native).
  Setting it to empty clears it. New tables default to managed when unset.
- `create` requires a missing registration, empty application prefix (all key
  types, including nested keys), and unused index name. Explicit string __key
  values are required; all other fields form the registered Arrow-derived schema.
- Create a minimal application-owned membership index, with one NOINDEX TAG
  attribute and no hidden row-ID field. Use SKIPINITIALSCAN and recheck prefix
  emptiness after FT.CREATE; refuse rows that could have missed initial indexing. Column indexing remains application-owned;
  creation rejects the managed index_columns override. Existing table definitions/index
  names/prefix data are never deleted or silently reused.
- `create_append` creates if missing; otherwise appends to the existing unmanaged
  registration without changing schema/index/encoding. Explicit source identity
  and supplied encodings must match the existing registration. Existing unmanaged
  create_append also works without source configuration.
- Coordinate registration creation before input consumption using the existing
  leased catalog admission pin; release it before direct row writes. Registration
  metadata commits before input rows. There is no source-data publication step.
- Failure may leave a new application index/registration and acknowledged partial
  rows. Never automatically replay creation or input, or drop application resources
  during error cleanup. Preserve counts/status/SQLSTATE and explain recovery.
- Reject source overlap with driver storage, reserved columns, missing/invalid
  key schema, incompatible configuration, managed-mode conversion, temporary or
  virtual targets, shared snapshots, retained/staged writes, and replace before
  consuming input or changing source resources.
- No format/version change, upsert, collection replacement, or performance claim.
