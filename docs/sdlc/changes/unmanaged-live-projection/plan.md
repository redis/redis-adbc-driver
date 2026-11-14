# Plan

[Intent](intent.md), [spec](spec.md), [validation](validation.md).
Author: Codex. Human review pending.

1. Inspect dominant retrieval paths and why plain HMGET plus a separate existence
   query cannot preserve live deletion/type races.
2. Run an opt-in matched retrieval-only prototype across four-column and wide
   HASH fixtures, completely validating fields and aggregate arithmetic.
3. Add bounded atomic projection only after filtered unmanaged Search discovery;
   retain full LOAD-star and known-key behavior.
4. Verify exact values/existence, live mutation, source ownership, permission
   compatibility, cancellation, cursor lifetime, request bounds and no replay.
5. Compare complete typed Arrow results on the same verified Software fixture;
   retain regressions and allocation increases. Parent integration owns native
   Python/C-boundary one/four-client matrices and final combined-source review.
