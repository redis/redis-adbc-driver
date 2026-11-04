# Plan: bounded Substrait runtime

Date: 2026-11-04. Baseline: `f4d5b78`. Author: Codex.
[Intent](intent.md), [spec](spec.md), [validation](validation.md).

- [x] Review current protocol, generate exact bindings, pin sources and fixtures.
- [x] Implement strict wire/semantic validator and typed ordinal AST adapter.
- [x] Integrate statement state, metadata revalidation and C preflight.
- [x] Add shared scan/byte/deadline accounting and owned streaming/materialized results.
- [x] Add malformed-plan, exact-value, lifecycle, budget and fuzz tests.
- [x] Complete native driver-manager capability/execution tests and full regression runs.
- [x] Review against REVIEW.md and record remaining acceptance gaps.

[Agent review](review.md); maintainer review remains pending on the draft PR.

The original [acceptance matrix](../213-substrait/plan.md) remains the broader
checklist. Keep #213 open if required matrix coverage or supported-environment
validation remains incomplete; do not treat generated bindings or compilation
as closure evidence.
