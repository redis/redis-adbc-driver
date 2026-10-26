# Specification

2026-10-21; baseline `85d6807`; issue #377. See [intent](intent.md), [plan](plan.md).

- Validate FT.INFO indexing=0 and finite percent_indexed>=1 before/after opening
  bounded cursors and before/after page reads. Check every OSS Cluster primary;
  a proxy must expose the distributed readiness fields. No readiness caching.
- Building/incomplete state returns INVALID_STATE; unavailable/malformed fields
  return NOT_IMPLEMENTED; denied FT.INFO returns UNAUTHORIZED. No page from a
  failed check is exposed. Close any opened cursor; never replay/deduplicate.
- Ordinary lowered-cap planning retains complete HASH fallback when readiness
  is building/unknown/denied. Mid-stream changes fail explicitly. Preserve
  existing stale-prefix, query failure and cancellation errors.
- Workers require FT.INFO in addition to the existing read-only commands;
  configured caps do not bypass readiness, and CONFIG remains optional.
- Only shared bounded numeric scans change. Empty/direct non-Search results and
  other ordinary paths retain their behavior. These checks mitigate the observed
  backfill condition; concurrent external index replacement and unobserved backend
  defects are not made atomic by two metadata checks.
