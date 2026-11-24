# Plan

Date: 2026-11-23. Dependency: [PR #603](https://github.com/redis/redis-adbc-driver/pull/603).
See [intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Reconstruct only the guarded UNION/dimension changes on the public lazy-CTE PR head; exclude the investigation's private commit ancestry and all Flex/query/snapshot/parallel changes.
2. Defer eligible sources in `bind.go`; dispatch deferred trees in `view.go` and `deferred_union.go`; retain real join/residual semantics.
3. Add bounded dimension ordering and composite typed equality demand; preserve outer/lateral ordering and all unsupported fallbacks.
4. Preserve output-column identity through per-join qualification and explicit aliases in `join.go`/`lateral.go`; retain the review regression.
5. Run focused and full unit-only checks, inspect source/alias/error/snapshot boundaries, and request an additional independent agent review. Publish as a **draft**, with human review and live issue-targeted verification pending.

Future work is separate: a page-oriented deferred UNION scan API and immutable physical-row bounds may support aggregate consumers without retaining the full fact output. They must preserve later-source versus consumer-error order, aliases, full residuals and snapshot proofs. No streaming files or aggregate hook from that follow-up are included here. Full suites remain stopped.

Draft implementation: [PR #606](https://github.com/redis/redis-adbc-driver/pull/606).
