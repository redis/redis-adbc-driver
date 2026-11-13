# Plan

See [intent](intent.md), [spec](spec.md). Base v0.0.26 / 5c9d583a.

1. Reproduce missing Arrow append using the Software integration harness.
2. Dispatch unmanaged input outside managed workspace/publication; reuse typed
   conversion and checked source INSERT, with bounded slices and failure counts.
3. Cover HASH adoption and foreign registrations, refusal before consumption,
   key/encoding/NULL behavior, partial and uncertain failures, cancellation and
   source ownership. Recheck managed Arrow admission/publication regressions.
4. Run focused and appropriate full checks against dedicated Redis Software with
   effective verified 4x QPF. Document real skips and test outcomes.
5. Update feature/user docs and invariants, review the diff, create a separate PR
   from the benchmark evidence PR, and remove task-owned resources.

Implementation and local checks completed; see [validation](validation.md).
The isolated fixture is being reused for the authorized create/create_append follow-up.
