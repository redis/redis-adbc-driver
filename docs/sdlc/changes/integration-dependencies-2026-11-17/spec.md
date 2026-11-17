# Spec

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Source: `75220f2f` plus the finite numeric candidate (unrelated to these paths).
Status: local checks complete; upstream fixes remain open.

Preserve empty ADBC parameter field names and parameter values. Compare unmodified CRAN adbi with the isolated pinned patched recipe using one/two/NULL/mixed bindings and rebinding. Reproduce empty default Dataset persistence independently of Redis; verify actual ADBC mitigation without broadening strict expected-failure guards. Upstream fixes must be recorded as pending until accepted and qualified.

See [intent](intent.md), [spec](spec.md), [plan](plan.md), [validation](validation.md).
