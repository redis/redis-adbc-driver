# Specification

2026-10-21; issue #380; see [intent](intent.md), [plan](plan.md).

- Infer only exact signed integer literals/typed parameters, unary signs, and
  internal __rowid equality/inequalities (either operand order), finite IN and
  BETWEEN. AND intersects known constraints; OR requires all branches known.
  Unknown float/decimal/text/NULL/casts/functions/NOT/mixed OR retain the full
  candidate domain, except independently safe conjuncts. Recheck the full SQL
  predicate on authoritative values; unrelated business IDs are not internal IDs.
- Intersect with the captured allocated-ID domain below 2^53. At most 4096
  expression nodes, 128 nested levels and 1024 disjoint intervals; inference
  budget exhaustion only widens candidates. Do not enumerate >1024 IDs.
- Finite candidate sets split into at most partition_count disjoint groups;
  empty sets emit one schema-carrying empty descriptor. Larger interval unions
  use a conservative envelope; default/balanced cuts operate within it.
  Descriptor version and incremental discovery/reset contracts stay unchanged.
- At read time, proven sets of <=1024 candidate IDs use HASH pipelines of <=1000
  keys and the shared Arrow/context/connection owner machinery. Buffered rows
  are bounded by the configured Arrow batch plus one pipeline page. Check
  current physical identity before/after HASH reads; preserve exact types,
  residuals, missing/deleted rows, cancellation and prompt release.
- Search-free reads do not need CONFIG, FT.INFO, FT.SEARCH, FT.AGGREGATE or
  FT.CURSOR. Connection startup/metadata and HASH permissions still apply.
  Search-backed fallbacks preserve cap/readiness contracts and explicit errors.
  Descriptor validation, including invalid cap rejection, still applies.
- Ordinary row-ID equality cannot round a non-integer into a direct key and
  discard its equality predicate. Such values retain normal numeric planning.
- Record nonempty exact value/schema comparisons, descriptor/Redis command
  counts, planning/full-fetch timing and sampled memory, including process
  startup and IPC. Supplemental ClickBench-source SQL is not leaderboard SQL.
