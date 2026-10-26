# DROP lists, arrays, set-returning functions and explicit-range reads

2026-10-22. Base `403e98f`. Requestor alberttwong; author Codex.
Status: implemented; ready for human PR review. [Spec](spec.md), [plan](plan.md).

Implement #262, #253, #256 and #448 in one PR. Complete the existing scalar-array
foundation, reuse the bounded GENERATE_SERIES/lateral machinery for SRFs, preflight
multi-object drops, and expand managed wide-scan eligibility only after measuring
and verifying exact SQL semantics. No transaction/snapshot or new server API claim.
