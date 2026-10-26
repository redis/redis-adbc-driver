# Plan and evidence

2026-10-22; source `aab9612`; see [intent](intent.md), [spec](spec.md).

1. Inspect the two open bug issues and existing implementations/tests.
2. Capture focused failing all-NULL set-operation cases, then resolve their
   type at the set-operation planning boundary.
3. Add Redis/Arrow integration checks for resolved NULL schemas and the full
   repeated-key query/deletion shape in all three pushdown modes.
4. Run focused checks, the full sequential Go suite and native library build.
5. Review correctness, ownership and evidence using repository REVIEW.md.

## Verification

- Before the change, [regressions](evidence/before.log) failed: all-NULL set
  operations retained type NULL, and a subsequent integer UNION succeeded.
- After the change, [focused checks](evidence/focused.log) passed, including
  all three #232 modes, exact DELETE row counts and resolved Arrow schemas.
- Initial baseline unit-only suite passed all three Go packages; Redis tests
  skipped as expected. The post-change [full Redis suite](evidence/full-suite.json)
  passed all three packages: 1,194 tests/subtests passed, zero failures, 20
  skips for opt-in profiling, special fixtures and subprocess helpers. Main
  package: 163.569s; CLI: 0.851s; benchproxy: 0.154s. The summary records every
  skip reason.
- `make -C go build` passed with the declared version linker flag.
- Test environment: Go 1.27.2, macOS arm64; dedicated disposable official
  `redis:8.6.2` container at localhost:18445, separate from existing benchmarks.
  No Cloud resources created; disposable container removed after verification.
- `gofmt` and `git diff --check` passed. An optional race-detector pass was
  stopped during compilation to keep Redis suites sequential; no race result
  is claimed.
- The external dbt adapter test suite has not been run. No downstream xfail
  markers or issue states changed.

## Review

The planner change affects only a common type still equal to NULL, after
checking branch compatibility. Existing typed NULL branches retain their
declared type. The shared set-operation execution already coerces values to
the planned output type. No storage writes or lifecycle paths were modified.
Tests fully fetch Arrow readers and check row values and schemas. Review by
Codex is an implementation check, not maintainer approval.
