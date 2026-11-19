# Plan and verification

Date: 2026-11-18. Status: implemented and locally verified; PR checks pending.
See [intent](intent.md) and [specification](spec.md).

1. Inspect Arrow ingestion, live SQL defaults, source INSERT batching, execution
   cancellation, and existing acknowledged-row errors.
2. Capture a focused failing live Arrow append regression before implementation.
3. Add an isolated live append path with bounded conversion and source writes;
   share existing conversion and source INSERT machinery without changing
   managed publication or unmanaged ownership rules.
4. Add meaningful coverage for defaults/schema checks, failure/cancellation,
   bounded partial outcomes and source/metadata/index preservation.
5. Run focused tests and regressions on Redis Software with verified 4x QPF,
   unit checks, source builds, and a native Arrow smoke check when available.
6. Update the live guide, ownership/feature tables and invariants; record actual
   validation and self-review. No release or maintainer approval is implied.

All local steps completed. See [validation and review](validation.md). The PR
will carry remote checks and any follow-up fixes before this task is complete.
