# Map and Union storage contract

See [intent](intent.md), [plan/evidence](plan.md), and the
[public Arrow contract](../../../arrow-interoperability.md).

- Arrow-ingested MAP stores an ordered sequence of key/value pairs. Keys use
  supported canonical scalar SQL types and must be non-NULL; Null/composite key
  schemas reject even for empty input. Duplicate keys remain separate entries.
  Values can be NULL or recursively supported containers. Missing HASH fields
  represent NULL maps; an encoded empty entry sequence represents an empty map.
  Output is Arrow Map with canonical `entries`, `key` and `value` field names,
  non-null keys, nullable items, and `keys_sorted=false`. Item nullability is
  canonicalized to true on every interface; input non-null item declarations
  are not retained.
- Arrow-ingested UNION stores an integer type code and exactly one selected
  child cell. Dense offsets and sparse aligned children normalize to dense
  Arrow Union output. Persist child order, names, canonical types, nullability
  and the explicit unique codes in 0..127. A selected-child NULL remains a
  non-NULL tagged value. Arrow has no parent Union validity: a SQL NULL Union
  produced by SQL uses Arrow's first-child NULL representation on output.
- Both types are discoverable through Arrow-ingested schemas and CTAS, not new
  SQL constructor/type-declaration syntax. Names follow stored-struct limits:
  unique, nonempty, at most 1024 bytes, no NUL. Unions have 1..128 children.
  Arrow input nesting is at most 16 levels. Persisted column schemas are at most
  1 MiB and 64 JSON nesting levels before recursive decoding/interning.
- HASH cells have a version-1 JSON envelope with `v`, `tag`, and `fields`.
  Map fields alternate key and item cells; Union has one selected child cell.
  NULL children are JSON null; other recursively encoded cells are base64
  byte strings. Map tag is zero. Parent struct/list encodings remain unchanged.
  Stored Map/Union cells reject unknown versions, invalid shapes/tags, NULL
  non-nullable children, more than 1,048,576 cells or over 128 MiB encoded input.
- Append requires identical canonical schemas, including nested schemas,
  names/nullability and type-code mapping, even for zero/all-NULL input.
  Sparse/dense input and normalized scalar-width changes are compatible when
  the canonical schemas match. Schema evolution and automatic casts reject.
- Map comparison is lexicographic over the entry sequence, including duplicate
  keys and entry order. Union compares numeric tags, then the selected value.
  Both require identical schemas. Nested NULLs sort last. Grouping/distinct
  keys include container schema and Union tag. Field notation/key lookup and
  scalar text/JSON functions reject; COUNT/ANY_VALUE and schema-matched
  COALESCE/NULLIF/GREATEST/LEAST preserve generic value behavior.
- Neither container is implicitly indexed or pushed down to Search. Comparisons
  use residual evaluation. Ordinary output and stream/file IPC preserve the
  canonical schema. Live partitions support independent replay; existing
  restrictions on bound container parameters remain. Retained execution rejects
  any source/result type containing Map/Union before publishing a descriptor.
- Structural validation precedes logical decoding. Batches containing Map or
  Union share the normalization limits of 1,048,576 decoded cells and 64 MiB
  string/binary payload across all columns, counting selected Union children,
  Map entries and expanded dictionary/run-end occurrences. These are per-batch
  limits, not process memory guarantees. Earlier stream batches are not rolled
  back if a later batch fails. Strings/binary values own their bytes after
  releasing input; retained output batches survive cancellation/release.
