# Intent: installed-wheel Substrait C validation

Date: 2026-11-04. Baseline: `4fd0894`. Author: Codex; requested by Albert Wong
while investigating failed Actions after PR #316.
[Spec](spec.md), [plan](plan.md), [validation](validation.md).

Restore release validation on Linux amd64/arm64 and macOS arm64 without
weakening the Substrait pointer/size safety assertions. The C harness assumed a
Go-generated header lived beside the tested library. Installed wheels contain
the library only and can rename it, so all three platforms failed compilation.
