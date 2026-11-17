# MIN_SCALE implementation plan

Date: 2026-11-17. Source: `19ebcb46`. Status: implemented and locally verified; ready for PR review.
See [intent](intent.md), [spec](spec.md) and [validation](validation.md).

1. Retain focused failing scalar regression before registering the function.
2. Add a narrow planning/runtime signature guard, int32 inference and exact
   coefficient/scale evaluator; retain existing shared numeric input validation.
3. Cover NULL/unknown/typed-invalid/bound/empty inputs and input ownership.
4. Compare finite values and exact errors against PostgreSQL 18.6; retain numeric
   special values as an explicit exclusion rather than expanding #362's scope.
5. Run focused unit/race and registry checks; parent task runs managed/bound
   integration checks on owned Redis Software with verified active/effective 4x QPF.
6. Review diff and evidence; keep #110 open for its remaining acceptance.
