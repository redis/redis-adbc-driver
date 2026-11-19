# Specification

Date: 2026-11-18. Status: implemented; see [validation](validation.md).
See [intent](intent.md) and [plan](plan.md).

- Enable `adbc.redis.live_tables=true` and target a registered table in
  `redis_live`. `append` and `create_append` append to that existing registration;
  an absent registration is an error. Creation and replacement, temporary
  targets, retained operations, shared read snapshots and unmanaged-source
  configuration are refused before advancing the Arrow input.
- Map Arrow fields to the fixed registered SQL schema using existing Arrow
  conversion/coercion and extension policies. Reject unknown/duplicate input
  columns and non-string `__key` schemas before reading input batches. An
  explicit NULL key, out-of-prefix/nested key, existing key, or duplicate key
  is refused. Missing public fields receive normal SQL NULL/default handling.
- Omitted `__key` uses the live virtual column's volatile SQL default
  `<registered-prefix> || GEN_RANDOM_UUID()::TEXT` once per input row. Generated
  keys are not stored as HASH fields. A row must have a non-NULL stored field;
  UUID generation alone cannot create an empty HASH. Ingestion returns a count,
  not a generated-key result set.
- Convert at most 4,096 rows at once. Submit bounded source INSERT pipelines
  using the existing 128-row/1 MiB source budgets and 64 MiB single-row ceiling.
  Reuse atomic single-key collision refusal, preserve exact values, and check
  registration identity before submitting each pipeline. Never replay input or
  unknown mutations. Existing source data, TTLs, unmodeled fields and all index
  definitions are unchanged by append.
- Live ingestion has per-row/partial-write semantics. Count every positive
  acknowledgment in an already submitted pipeline, even after another reply
  fails. Return the acknowledged-row lower bound on errors, preserve original
  status/SQLSTATE, and include `adbc.redis.ingest.acknowledged_rows`. A lost reply
  can hide an applied row. Do not continue to another pipeline or batch after
  failure, cancellation, schema drift, or reader error.
- Honor statement/connection cancellation and Arrow ownership. Release bound
  input references on all live execution paths. Do not allocate a managed
  workspace, acquire managed write admission, publish a generation, register
  live rows with GC, or alter behavior for managed/unmanaged ingestion.

Acceptance covers explicit/binary and generated keys, streams and conversion
slices, column order/nullable values/exact integers, malformed schemas, protected
source keys/TTLs, collisions and partial errors, uncertain replies, cancellation,
input release, and unchanged managed/catalog/index ownership. Redis tests must
use Redis Software with verified active 4x QPF. No throughput claim is made.
