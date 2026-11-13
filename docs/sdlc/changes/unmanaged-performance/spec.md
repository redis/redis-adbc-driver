# Contract

See [intent](intent.md) and [plan](plan.md). No storage-format change.

- Unmanaged INSERT/Arrow append retains full conversion-slice key/type preflight.
  Source writes use single-key atomic EXISTS/HSET scripts in bounded pipelines
  (128 commands, 1 MiB request target, 64 MiB single-row ceiling). Keys may occupy
  different cluster slots. Catalog pin validation occurs before each pipeline.
- Every submitted mutation carries NoRetry; never replay a pipeline or input.
  Inspect every reply, count every acknowledged success, preserve the first
  failure's status, and stop before submitting the next pipeline. A submitted
  pipeline may apply rows after a conflict, cancellation or lost reply. Counts
  are lower bounds, not a contiguous prefix of input. Source writes remain
  individually atomic, with no rollback or managed publication.
- At statement planning, reread and validate the source index. Only compatible,
  native integer NUMERIC attributes may prune predicate candidates. Recheck the
  complete SQL predicate on authoritative HASH values. Text encodings, TAG,
  floating-point and incompatible/aliased/NOINDEX attributes retain fallback.
  Do not enable index aggregate/sort value shortcuts for mutable source values.
- Filtered unordered unmanaged scans may discover keys through Search and fetch complete
  HASHes in bounded pipelines. Preserve membership/FILTERs, binary keys, nested
  exclusions, NULLs and unrelated fields. Vanished/non-HASH keys are omitted.
  Full scans retain LOAD *. Flex retains SCAN/INKEYS enumeration and full SQL evaluation.
- Verify actual Software 4x QPF before integration tests. Test range candidate
  reduction, source changes, encoding/index fallbacks, error counts, races,
  cancellation and no replay. Performance claims require completed, validated
  results and labeled settings; historical results remain unchanged.
