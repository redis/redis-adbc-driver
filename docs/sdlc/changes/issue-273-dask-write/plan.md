# Plan

2026-11-17. Source: `19ebcb46`. [Intent](intent.md), [spec](spec.md),
[validation](validation.md).

1. Add lazy sequential worker-local Arrow append tasks and serializable failure
   diagnostics, with mandatory duplicate-write acknowledgment.
2. Add unit lifecycle/count/schema/refusal/recompute cases and separate-process
   native Software acceptance, retaining existing read regressions.
3. Document table ownership, concurrent writers, memory, failures and replay.
4. Run focused Python checks and native tests only after management verification
   of active/effective 4x QPF on the dedicated Software fixture. Inspect diff and
   record actual outcomes before proposing closure.

All four steps completed. Focused and full native results, independent agent
review, and explicit acceptance limits are recorded in [validation](validation.md).
