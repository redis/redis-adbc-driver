# Plan

Date: November 13, 2026. Author: Codex. Source: ca685806. Status: implemented and focused validation complete.
[Intent](intent.md), [specification](spec.md).

1. Inspect admission, final-index construction and publication command paths.
2. Remove repeated descriptor discovery, combine independent directory reads,
   and omit abort only after confirmed publication.
3. Add focused deterministic discovery-budget/failure tests and native regressions
   for exact descriptor handoff, old-reader visibility and failed/no-op cleanup.
4. Run focused tests and relevant existing snapshot, index and ingestion suites
   on parent-owned verified Software 4x QPF in a serial live slot.
5. Integrate with other optimizations, run native scenario before/after matrices,
   record actual evidence and review unresolved risks before PR completion.

[Validation and review](validation.md).
