# Exact execution contract

2026-10-21; baseline e692a70. See [intent](intent.md), [plan](plan.md).

- Recognize integer <>/!= and their reversed comparisons. Push two positive
  numeric ranges to exclude NULLs. Use strict exact bounds; when rounding or
  precision makes the bound inexact, include the boundary on both sides and
  retain residual checking. Preserve missing-default widening/recheck. Other
  types retain their existing paths.
- Reuse one typed scalar input vector only for managed, non-grouped, direct
  COUNT/SUM/AVG/MIN/MAX column inputs, consumed synchronously. The callback cannot retain the vector; accumulators keep owned values. Unsupported ownership, residuals, expression inputs, explicit keys,
  SCAN, sorting, ranges, late materialization and cleanup retain scanPages.
  Expressions keep whole-page decode/error order, including serial replay.
  Authoritative HASH reads still protect BIGINT precision; no approximate sums.
  When every needed field/null marker is already fetched, decode its owned HMGET
  positions directly. Keep existing narrow/wide pipeline bounds; mixed Search/
  HASH inputs retain raw-row merging. No new key discovery or selection rule.
- Parse identity list-cardinality reducers directly from owned RESP2/3 containers
  while retaining exact list cardinality, scalar conversion, duplicate/NULL and
  malformed-reply rules. Nonidentity aliases retain generic normalization.
  No DISTINCT collision/presence checks, tuple grouping or global merge are removed.
- Prove source-equivalent values/types/errors, sparse/default/NULL/boundary cases,
  both protocols, standalone/Cluster, cancellation and bounded spill/ownership.
  Repeat pinned full SQL/Arrow reads for Q2/Q4/Q9/Q10 on the same full data, with
  independent references, raw failures, client/server CPU/traffic, allocation,
  peak memory, spill and actual plans. Keep allocation and wait metrics scoped.

- For unfiltered managed wide tables (at least 64 columns), direct integer tuple
  inputs of at most four columns with DISTINCT and inexact index loads may use
  authoritative narrow HASH reads and bounded local complete-tuple compaction.
  Cap the map at 4096 entries and flush to the unchanged exact global spill
  merge; never sum local distinct counts. Preserve NULLs; columns with historical missing defaults retain the existing tuple-shape fallback.
  Select only after three PING samples per primary all complete below 2ms, with
  a 500ms probe context and 100ms read/write timeout clone. Slower/failed probes keep the established server source.
  This conservative source heuristic needs both local and delayed-link evidence.
