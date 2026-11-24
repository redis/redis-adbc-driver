# Intent: avoid unnecessary shared CTE alias copies

Date: 2026-11-23. Base: `0bed7ce5`. Status: isolated prototype, not published.
Requestor: repository user through parent DS investigation; author: Codex.

scanJoinItem currently allocates a qualified map for every row of a shared CTE
before its alias-local residual removes other years. Reduce those copies only
when integer/bool necessary predicates can safely precede allocation, retaining
the original complete residual. This is a Q4 memory prototype, not a live result.
See [specification](spec.md), [plan/evidence](plan.md).
