# Plan

1. Read production and existing unmanaged projection/evaluator contracts. Bind
   only a small pure integer/boolean subset; use ordinary comparison primitives.
2. Preserve decode-all ordering and bind needed predicate inputs once. Feed
   accepted output into existing Arrow ownership/batch/limit handling.
3. Capture a baseline failing projection-selection regression, then run focused
   unit/race and verified Software live retained-buffer/mutation/error checks.
4. Build committed candidate with explicit version. Compare baseline/candidate
   native full scans and filtered four-reader scans with complete independent
   type/value/multiplicity validation, retaining all trials and negative results.
5. Publish sanitized evidence only; never overlap tests/builds/timing. Parent
   coordinates the shared owned Software fixture and cleanup.

See [intent](intent.md), [contract](spec.md) and [validation](validation.md).
