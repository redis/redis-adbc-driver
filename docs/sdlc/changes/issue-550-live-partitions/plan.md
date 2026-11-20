# Implementation plan

Date: 2026-11-20. Requestor: Albert Wong. Author: Codex.
Source baseline: `845c95b4`. Status: implementation/verification in progress; no human approval.

[Intent](intent.md) · [Spec](spec.md) · [Verification](verification.md)

1. Add explicit source-mode admission and canonical v6 descriptors with existing partition group lifetime.
2. Add bounded raw-key bucket scan, live identity/index checks and Arrow stream cleanup.
3. Exercise exact values/keys, residuals, independent workers/processes, source changes, stale identities/indexes, expiry/release, cancellation and budgets.
4. Run focused Redis Software tests with verified active 4x QPF; run unit checks and shared partition/live regressions.
5. Document limits, live consistency, O(partitions × source scan) discovery cost, and retained/Flex exclusions; self-review and open a focused PR.
