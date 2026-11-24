# UNION and star visitor integration

Date: 2026-11-23. Origin: streaming follow-up to
[PR606](https://github.com/redis/redis-adbc-driver/pull/606).

The single scanPages hook now visits guarded UNION sources before ordinary
memory materialization. Star shape preflight is executor/context aware: only its
largest fact may be a guarded deferred UNION with a known immutable upper bound;
all retained dimensions keep the original direct-source proof. Qualified fact
views route through that hook before the ordinary memory-table branch.

Ordinary parsed join items have bookkeeping presence tags. Refuse streaming
only when the consumer requests that presence dependency; unsupported whole-row
conditions still refuse during predicate preflight. Parsed ordinary and Q2-style
weekly CASE regressions assert actual stream selection, so functional results
cannot silently validate only the eager fallback. Outer/lateral eligibility
remains unchanged.

After shape preflight and before source scans, diagnostics record only
`kind: streaming_star_join`, `dimension_count`, `output_page_rows`, and phase
`streaming_star_join`. A filtered dimension-cap fallback replaces this marker
with `eager_join_fallback` / `filtered_dimension_cap`, avoiding a false active
stream claim. No physical names, prefixes, IDs or infrastructure values appear.

The standalone publication branch runs parsed-plan selection, exact result,
source-error, retained-budget and page-boundary regressions. See
[verification](../streaming-star-aggregate/verification.md). Live query outcomes
remain separate evidence; no successful Q2 or Q4 benchmark is implied here.
