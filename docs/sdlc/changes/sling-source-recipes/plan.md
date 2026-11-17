# Plan

Date: 2026-11-17. Source: `5fe2bb76cacca9a385479f56b739aeb8e63b3838`.
Requestor: user in this Codex chat. Author: Codex. Status: complete; statically verified.
[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Compare the worktree with current main and read repository/test invariants.
2. Check official Sling connector docs and versioned implementations for setup
   options, MongoDB collection reads and warehouse export behavior.
3. Expand `docs/integrations/sling.md` with shared setup and six source sections,
   expected values, ownership-aware cleanup and validation scope.
4. Check Markdown links/anchors, shell and embedded configuration syntax; review
   the diff against the specification and record actual results.

No driver build or live Redis/warehouse tests are needed for this docs-only
change. Check results and evidence limits are recorded in [validation](validation.md).
