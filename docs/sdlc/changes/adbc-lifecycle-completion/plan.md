# Plan

2026-10-22. Author: Codex. Base: 7a62902. Status: implementation, verification and self-review complete;
[PR #496](https://github.com/alberttwong/redis-adbc-driver/pull/496) open for review.

1. Inspect merged follow-up, current issues, REVIEW.md and public contracts.
2. Reproduce full-suite lifecycle failures before correcting them.
3. Cover partition discovery/replay with isolated transport ownership; fix
   diagnostics, independent maintenance/rollback and child-context recovery.
4. Expand fault matrix and run with race detection. Run full Go and native
   checks sequentially against verified Redis Software 4x QPF.
5. Reconcile public documentation, record evidence/skips/self-review and open
   a reviewable PR. Keep source-snapshot feasibility and #269 dependencies clear.

The original full-suite run reproduced type-rewrite-close, rekey-close and
socket-diagnostics failures. The existing deterministic assertions are retained.
The retained reader close-registration test now injects its hook into the actual
execution pool rather than the bypassed historical connection-level read pool.

See [intent](intent.md), [spec](spec.md), [evidence](evidence.md).

All implementation/check steps are complete. Actual results, skipped capabilities
and deployment limits are recorded in [evidence](evidence.md).
