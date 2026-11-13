# Plan

See [intent](intent.md), [spec](spec.md).

1. Add a bounded explicit source option and typed creation preflight.
2. Create a fresh membership index without overwrite/replay, register metadata
   with create-only semantics, then use the checked unmanaged append path.
3. Cover creation/concurrent create_append, existing target identity, schema and
   encoding behavior, NOINDEX membership, ownership/partial-resource failures,
   unknown replies/cancellation, rejected operations and managed regressions.
4. Verify Software 4x QPF before live tests; run focused race/unit and Python/C
   checks, document results, create a follow-up PR and clean task-owned resources.

Implementation and local verification complete; see [validation](validation.md).
