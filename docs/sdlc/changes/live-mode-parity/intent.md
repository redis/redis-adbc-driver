# Live mode feature integration

Date: 2026-11-20. Requestor: Albert Wong. Author: Codex.
Status: implemented and verified locally; human review pending.

Integrate the independently developed live MERGE (#553), incremental queries
(#554), and compatible schema/index evolution (#555). Preserve application HASH
ownership when the features interact. See [spec](spec.md) and [plan](plan.md).
