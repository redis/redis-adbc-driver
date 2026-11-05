# Plan: adopted writes

Date: 2026-10-30. Source: `95d6583`. Author: Codex. Status: implementation
and local validation complete. [Intent](intent.md), [specification](spec.md),
[validation](validation.md), [issue #64](https://github.com/redis/redis-adbc-driver/issues/64).

## Files and order of work

1. Confirm KeyRows prerequisite in store/keyrows; retain application ownership.
2. Add an ephemeral __key input adapter and shared atomic insert writer in
   `go/adopt_write.go`; integrate SQL, MERGE, RETURNING and Arrow ingestion.
3. Add the statement-only opt-in and bounded, type-checked in-place truncate.
4. Add integration regressions for fields, keys, collisions and ownership;
   update existing refusal expectations and CLI checklist wording.
5. Update driver reference contracts; run focused tests, unit-only tests, full Redis
   Software suite, race coverage for competing inserts, and native build.
6. Review lifecycle, ownership, and documentation against REVIEW.md. Record
   actual evidence in validation; no human approval is implied.

## Risks and alternatives

Use explicit __key (one of issue #64's alternatives) rather than introducing a
metadata key-pattern language. Keep system columns out of joins to avoid
expanding that separate contract. Refuse mixed-table truncate lists because
managed generation switching and adopted in-place deletion cannot share the
managed path's all-or-none metadata transition. Document partial progress.

For PR submission, replayed this change onto current main `91064e6`; the
documentation now lives in `docs/driver-reference.md`. Rebase validation is
recorded separately from the original worktree evidence.
