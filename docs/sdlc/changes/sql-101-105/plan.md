# Plan

2026-11-03. Base `bb83527`. Status: implemented and locally verified; draft PR preparation.
[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Pull current upstream. The original detached checkout had unrelated history;
   preserve it and create `codex/issues-101-105` from upstream main.
2. Extend parser/AST, execution, metadata and Arrow representation for all five
   requested SQL areas. Reuse table generation and index update paths.
3. Add focused regressions in `go/issues_101_105_test.go` and registry coverage.
4. Run unit, full Software, focused race, shared-library build and ADBC validation.
   Resolve failures and record actual results, skips and remaining limits.
5. Review ownership, failures, namespace collisions and publication. Update public
   reference and submit a reviewable change; no issue closure or merge implied.
