# Plan

2026-10-22. Author: Codex; requestor: Albert Wong. Base: `6508626`.
Status: implementation, local verification and self-review complete;
[PR #488](https://github.com/alberttwong/redis-adbc-driver/pull/488) open.
Rebased onto `1b30837`; intervening changes contain no driver/runtime changes.

1. Read issue source and repository contracts; verify Redis Software 4x QPF.
2. Add statistics.go, focused Go/driver-manager tests and the scoped contract.
3. Extend retained string-slicing admission and exact Arrow comparisons; record
   the remaining live/retained/ordinary gaps and snapshot feasibility.
4. Enable context socket deadlines and normalize context/network timer races;
   inject blocked reads/pipelines, assert pool recovery and single attempts.
5. Run focused and full Go checks, native metadata/partition/cancellation tests,
   formatting/link checks and REVIEW.md passes. Record actual results/skips in
   [evidence](evidence.md), then publish a PR referencing the three open issues.

All five implementation/check steps are complete. The initial full run also
required correcting stale ACL test paths after the README/reference split;
the command assertions and existing skips are unchanged. Actual outcomes and
remaining deployment/permission limits are in the evidence record.

No benchmark or source reload is needed. No new deployment is requested.
No operational identifiers, credentials or license artifacts belong in this PR.
Full immediate cancellation and broader snapshot/type/source support remain
unresolved; this work must not automatically close those issues.

See [intent](intent.md), [spec](spec.md).
