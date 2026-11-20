# Plan

Date: 2026-11-20. Source baseline: `845c95b4`. Requestor: Albert Wong. Author: Codex.
Status: implementation and validation in progress; no human approval recorded.

1. Retain baseline Search-safe workers, adding exact-directory eligibility and forced per-worker ranges for native managed Flex/nonindexable/high-ID layouts.
2. Preserve full predicates, exact result merging and cancellation/error precedence.
3. Add focused integration and failure tests on dedicated Software fixtures.
4. Measure fixed/adaptive grants, document actual results and update coverage.
5. Run appropriate unit/race/build checks, review lifecycle/ownership/evidence, and open a PR without merging.

See [intent](intent.md), [spec](spec.md), [validation](validation.md).
