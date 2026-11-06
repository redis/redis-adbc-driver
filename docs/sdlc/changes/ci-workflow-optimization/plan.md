# Plan and validation

1. Retire the completed SIMD workflow and update its benchmark README.
2. Eliminate the repeated PR DuckDB build and Linux source suite in Release.
3. Add tested distribution selection to the existing complete-diff selector.
4. Extract wheel-client validation and separate Linux/macOS dependencies.
5. Remove unreachable Test steps, lint workflows and run focused tests.

Evidence inspected: GitHub Actions runs 42091371895 (Test), 42091371688
(standalone DuckDB) and 42091372063 (Release), all for PR head `570feaa`.
Test's Go suites took 5m21s on arm64 and 5m59s on amd64. DuckDB ran both directly
(3m12s job) and through Release (3m46s job). Release's ten distribution jobs took
roughly 2–2.5 minutes each; its macOS producer was queued at inspection time.
These are historical observations, not validation of the new workflows.

Local validation:

- `python3 -m unittest discover -s scripts -p test_ci_test_matrix.py -v`: 9 passed,
  including release selection, complete diff/rename handling and diff errors.
- `python3 -m unittest discover -s scripts -p test_distribution.py -v`: 7 passed.
- actionlint v1.7.12 over all workflows, with `-shellcheck=`: passed. Shellcheck
  was disabled; no claim of shellcheck validation is made.
- `git diff --check`: passed; no live SIMD workflow references remain in
  `.github/` or `benchmarks/`.
- Source review checked artifact names, reusable inputs, permissions, historical
  ref fallback and the publication gate. No actionable findings remain from
  this review; it is not independent maintainer approval.

No Redis runtime tests were rerun for these CI-only changes. Changed workflow
execution and actual elapsed-time improvements remain to be verified by PR CI.
See [intent](intent.md) and [spec](spec.md).
