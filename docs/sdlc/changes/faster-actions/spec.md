# CI execution contract

Date: 2026-10-20. Source: `c74ebeb`. Status: implemented locally; CI pending.
See [intent](intent.md) and [plan/evidence](plan.md). Issue/PR: see the pull request containing these records.

- Native and registry DuckDB jobs install ccache and restore compiler output
  separately by OS, architecture and build variant. Each cache is bounded to
  1 GiB and refreshed on successful runs under a commit-specific key. Compiler
  and source validation remain enabled; configured build trees are not cached.
- Build workers are bounded by logical CPUs, four workers, and a budget of
  3 GiB per worker (at least one worker). Linux uses available memory; macOS
  uses physical memory. Hosted runs must confirm memory use and cache hit rates.
- Existing PR path filters exclude Markdown files. Mixed Markdown/code changes
  still trigger matching workflows. Changes to the cache action trigger DuckDB
  CI. Release publication and manual dispatch triggers remain intact.
- A newer PR release run cancels its predecessor. Published-release and manual
  runs retain existing concurrency behavior.

No tests, matrix entries, assertions or package serialization settings change.
Cold caches still require compilation. Cache eviction and compiler changes may
reduce reuse. This change does not diagnose or fix the earlier DuckDB fixture
job failure, and does not claim a measured speedup.
