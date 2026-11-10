# PostgreSQL 18 query semantics: plan

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong. Source: `85509485`. Status: targeted join/whole-row slice implemented and validated; five #361 cases remain.

1. Preserve grouped FROM operands and USING alias namespaces in parsing and planning.
2. Add meaningful positive, error and outer-join regression cases.
3. Implement whole-row binding/output and independent composite comparison.
4. Address other retained #361 query mismatches where feasible.
5. Run focused checks, unit tests and the unchanged full comparison under the verified Software gate. Record actual counts and remaining failures, then review per REVIEW.md.

See [intent](intent.md) and [spec](spec.md).

## Outcome

Completed the grouped-join, USING-alias, whole-row and composite-comparison
work. All fourteen targeted #361 cases and one related JSON case pass. The
unchanged full corpus measures 589/651 (90.48%); see [validation](validation.md)
and the source manifest. The five remaining #361 cases are listed explicitly
there and remain scored failures. They are outside this completed slice;
#361 and #118 are not closed by this change.
