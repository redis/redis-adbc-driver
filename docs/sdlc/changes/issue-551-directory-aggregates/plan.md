# Plan

Date: 2026-11-20. Initial source baseline: `845c95b4`; rebased onto `1d0d0b3b`. Requestor: Albert Wong. Author: Codex.
Status: implemented and locally validated; awaiting human review. No release recorded.

1. Retain baseline Search-safe workers, adding exact-directory eligibility and forced per-worker ranges for native managed Flex/nonindexable/high-ID layouts.
2. Preserve full predicates, exact result merging and cancellation/error precedence.
3. Add focused integration and failure tests on dedicated Software fixtures.
4. Measure fixed/adaptive grants, document actual results and update coverage.
5. Run appropriate unit/race/build checks, review lifecycle/ownership/evidence, and open a PR without merging.

See [intent](intent.md), [spec](spec.md), [validation](validation.md).

All implementation steps are complete locally. The final regression result and PR review remain recorded in [validation](validation.md); no release or human approval is implied.
