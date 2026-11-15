# Plan and selection decisions: CRUD execution refactoring

See [intent](intent.md) and [spec](spec.md).

## Completed investigation and selection

1. Froze main `3b422454` as the unchanged runtime baseline, created isolated root
   and subagent worktrees, and provisioned an exclusively scheduled Software
   fixture through `make -C go redis-up`. Verified active/effective 4× QPF,
   trusted TLS/RESP3 and resource limits before live work.
2. Captured opt-in Go CRUD command/client-hook attribution and separate ordinary
   native cursor-stage and nested prior-result-close observations. Inspected the
   actual pinned driver-manager source. Retained distinct observer checkpoints;
   no Go/native median subtraction or server-CPU attribution is made.
3. Developed unmanaged known-key selected-field reads and a descriptor routing-
   hint prototype, with focused regressions and independent safety review.
   Corrected prototype execution-context, materialization-bound, native ACL and
   proof/error-precedence problems and retained the actual failed checkpoints.
4. Built and froze the prototype and baseline, then ran its explicit point-only
   A1/B1/B2/A2 target screen. Both one-client modes missed the predeclared 10%
   p50 gate; the unmanaged four-client p95 also rose. Archived the full prototype
   patch and its exact source/evidence and removed its runtime/tests from the
   selected tree. The screen includes the unmanaged projection and is not an
   isolated metadata-only effect or complete CRUD/query comparison.
5. Kept production scope to the independently checked unmanaged known-key row
   projection, including read and applicable DML row-matching scan paths with
   their full required-field sets. Kept snapshot/admission/write-publication
   semantics, prepared execution,
   ordinary GC and Arrow ownership behavior unchanged.

## Remaining final-build work

6. Freeze the projection-only candidate and its exact native library/CLI and
   harness identities. Earlier broad/prototype correctness checks remain tied
   to their original sources; they do not substitute for final selected-code
   verification.
7. Run appropriate final Software/race/snapshot and failure/ownership checks.
   Run complete matched native scenario windows and the dedicated registered/
   unregistered wide-payload projection control, retaining complete distributions,
   raw Redis controls, unsuccessful/slower results and all warmups. Keep builds,
   tests, profiles and benchmark windows exclusively scheduled.
8. Review correctness/lifecycle, security/ownership and evidence against the
   selected contract. Record the actual selected-build outcomes, source/settings,
   measured gains and limitations; do not present the rejected target as passed.
9. After all clients exit, verify ownership and empty/index-free data, remove
   only owned fixture/network/client tags/generated credentials, and verify
   preservation of preexisting resources. Publish sanitized report/audits and
   open the PR with actual validation and no inferred human approval.
