# Plan and evidence

2026-10-22. Base `403e98f`. [Intent](intent.md), [spec](spec.md).

1. Capture focused failing SQL cases and baseline explicit-range plan decisions.
2. Add preflight multi-object drop execution; retain single-object APIs.
3. Complete array grammar/storage/Arrow interfaces and scalar operations.
4. Generalize bounded SRF planning/execution, SELECT expansion and FROM syntax.
5. Expand only proven wide-range eligibility, then run independent exact-result
   timing controls. Retain regressions and unsupported query controls.
6. Run sequential Redis suites, PostgreSQL comparisons, native/Python checks and
   focused race tests; review correctness, ownership and evidence using REVIEW.md.
7. Commit/push and create one PR; human review precedes merge.

## Completed checks

- Full Go/Redis suite, all three packages: passed (`-count=1 -p 1`).
- Focused race suite including DROP/arrays/SRF, wide ranges, generic fallback,
  defaults, checks, RETURNING and ALTER: passed.
- Native ADBC: 380 passed, eight capability skips, three subtests passed.
- Python native/Arrow: five passed. PostgreSQL differential: 63/63 passed.
- Harness tests: 25 passed. Host and declared Go 1.26.10 unit suites passed;
  the Go 1.26.10 native build also passed all 63 differential cases.
- Independent complete-fetch performance gate: all 96 trials validated.
- `git diff --check`: passed; changed Go sources formatted with gofmt.
- [Verification, retained evidence and three-pass self-review](verification.md).

Original source base is `403e98f`. Main advanced to documentation-only `c95ea98`
during implementation; no code differences were introduced by that update.
PR creation and human review are the final handoff; no merge is authorized here.
