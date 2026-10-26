# Intent: repair lifecycle CI regressions

2026-10-22. Requestor: Albert Wong. Author: Codex. Base: `4e26896`.
Status: implemented; verification running. See [spec](spec.md), [plan](plan.md).

The latest Test and native Release runs fail in close/rollback ordering,
execution socket diagnostics and cursor cleanup after cancellation. Preserve
the existing deterministic assertions and isolate these repairs from the larger
ADBC feature work. No supported-server or SQL semantics expansion is intended.
