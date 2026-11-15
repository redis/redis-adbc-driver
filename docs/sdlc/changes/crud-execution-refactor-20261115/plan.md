# Plan and selection decisions: CRUD execution refactoring

PR: [#443](https://github.com/redis/redis-adbc-driver/pull/443). Human review is pending.

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

## Final-build execution and review

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

## Actual outcomes

The [final report](../../../../benchmarks/crud-execution/results/2026-11-15/README.md)
contains all eight native ABBA jobs: 672 ordinary plus 512 width trials, all
validated, with complete pooled/pair/control distributions and frozen source,
library, CLI, harness and environment identities. The measured candidate is
`a944c99c500f2f5904ff669bea5cb9fb9b6b8e39`; later changes affect tests,
diagnostics and documentation only. Runtime selection excludes the metadata
prototype and includes only eligible unmanaged required-field retrieval.

No meaningful general CRUD latency improvement was demonstrated. Ordinary
one-client reads remain about 2 ms; wide one-client medians improve only
0.37–2.51%, with mixed and slower four-client/tail results. Selection is for
avoiding unnecessary source values, not a passed shared-read latency gate.
The complete results retain regressions and raw control drift. No performance
claim is made for unchanged managed publication or analytical paths.

Final ordinary Software checks: 2,630 pass events, 694 skips, zero failures
across ten packages. Live unmanaged race checks: 254 pass events (91 top-level),
six opt-in diagnostics skipped, zero failures. Separate test-only DML RETURNING
regression: parent plus two stream cases passed under race, no skips/failures.
The post-timing diagnostic helper correction passed two endpoint-free race tests.
Python suites passed 14 CRUD plus nine width tests; the public diagnostic auditor
reproduces all summaries from retained raw inputs. Earlier experimental checks
remain labeled by their actual checkpoints rather than final selected-code tests.

Independent subagent source review found no actionable correctness/lifecycle,
security/ownership or required-field dependency defect. Agent review does not
constitute human approval. Performance findings limit the claim to source-value
projection; the broad latency objective remains unmet. No normal GC policy,
foreign-buffer lifetime, prepared-plan or storage-protocol change was selected.

Owned fixture/network/client tag and generated credentials/owner files were
removed after exact marker checks and empty/index-free trusted TLS validation.
All preexisting image IDs and 29 inventoried network configurations remain;
one unrelated previously empty network was absent at final inspection and was
not named by the scoped teardown. Cause/timing is unverified. The initial
post-teardown inventory assertion failure and follow-up limitation are retained
in the cleanup record. No unrelated resources were recreated.

PR #443 is open with the complete evidence. No merge or release is performed
by this change. Human acceptance is pending.
