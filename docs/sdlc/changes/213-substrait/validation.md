# Evidence: Substrait design milestone

Date: 2026-10-30. Source: `95d6583d2b07bdfdadc6ddf3b6368bf631139c24`.
Author: Codex. Status: documentation/source review only, no human approval.
[Intent](intent.md), [spec](spec.md), [plan](plan.md),
[issue #213](https://github.com/redis/redis-adbc-driver/issues/213).

## Inspected evidence

- Issue #213 explicitly requests a design/feasibility milestone first. The
  historical README matrix anchor referenced by the issue was absent from the
  original source baseline. The change was then applied to upstream `main` at
  `de34042`, whose feature matrix also reports Substrait as unsupported; the
  setter and feature flag were rechecked there before creating the PR.
- `statementImpl.SetSubstraitPlan` unconditionally returns NotImplemented.
  `newDriverImpl` registers InfoVendorSubstrait=false. `go/go.mod` has protobuf
  but no Substrait library. No runtime support is added here.
- Reviewed statement prepare/parameter/schema/cancel paths; `SelectStmt` and
  expression nodes; `resolveTable`/`planSelect`; table schema discovery;
  `planWhere`/scan execution; `ColType`; and C `RedisStatementSetSubstraitPlan`.
  The C path copies plan bytes before reaching the Go setter, so future limits
  must be enforced on both sides of that copy.
- Retrieved upstream Substrait v0.61.0 algebra/type definitions and comparison/
  boolean YAML via GitHub. Inspected version fields in the pinned plan schema.
  Live website documentation has evolved (including extension identifiers), so
  the proposal uses release-pinned files rather than treating current website
  semantics as the selected protocol.

## Checks and limits

Checks performed:

- `git diff --check`: passed for the tracked documentation changes; a separate
  whitespace scan of all five new Markdown files also passed.
- Python relative-link check: all 27 local links in the five new documents
  resolve. The README and compatibility-table links to `docs/substrait.md`
  resolve too.
- Source/spec review: confirmed Decimal128 mapping at precision <=38,
  timestamp Arrow units/timezone, the absence of an i8 Kind, Project/emit order,
  cast failure enum and the candidate function names against the pinned files.
- Review against `REVIEW.md`: no runtime behavior or ownership change. Remaining
  semantic, security and lifecycle risks are explicitly implementation gates;
  no maintainer approval or runtime compatibility result is implied.

No Go source or runtime dependency changes; no Go tests, Redis integration,
C build, benchmark, fuzzing
or driver-manager validation was run. Runtime acceptance remains pending in
the [plan](plan.md). This evidence does not close those acceptance gates or
justify reporting Substrait as implemented.
