# Execution plan

See [intent](intent.md), [spec](spec.md). 2026-10-21. Final status: complete for #406, #410 and #411.

1. Implement and test optional accounted-byte batching in both stream paths and
   both public APIs. Sweep width, strings and budgets; report memory/latency/speed.
2. Extend controlled network benchmark evidence for Search LOAD versus HMGET;
   retain raw trials, exact results, CPU/GC/traffic and bounded routing behavior.
3. Reproduce aggregate plans and profiles, implement safe improvements, and prove
   Q2/Q4/Q9/Q10 on current full data after recovery/restore gates are satisfied.
4. Run focused/full Go, native and standalone/Cluster/race validation appropriate
   to changes. Perform all three REVIEW.md passes, publish PRs and evidence, and
   update issues only when their acceptance criteria are met. No merge or human
   approval is inferred.

The following chronological log retains earlier checkpoints and their then-open
gates; the final completion entry records the current status.

#411 audit: closed 2026-10-21 after verifying already-committed #419 controlled
network calibration/matrix, projection/batching screens and bidirectional byte
accounting, plus merged #422 positional allocation follow-up. Earlier chat status
understated this evidence. Closure links both reports and keeps all limitations.

#410 development: opt-in accounting implementation and focused tests pass.
Initial 16 MiB screen used a first implementation with redundant coercion and
all-column accounting, alongside correctness work; it is preliminary, not final
speed evidence. Removed redundant coercion, prebound variable-width slots, and
kept generic pending-byte updates linear instead of rescanning each page per
small output batch. Final quiet-host confirmation/sweeps are running separately.

#406 full dataset: read-only inspection finds one lost primary under recovery;
47-shard salvage is still in progress. Source for 2,081,569 missing rows is
prepared, but backup completeness/restore verification are not yet satisfied.
Preserve that work and historical outputs; no benchmark against a damaged dataset.

#410 completion: source 8a3f846 implements the opt-in budget; default row-only
policy stays unchanged. Final confirmation/sweeps contain 148 exact-value and
multiplicity-validated executions (32 warm-ups, 116 measured), including 4/16/64
MiB budgets, three/105 columns, varied UTF-8/NUL lengths and disabled controls.
See [report](../../../../benchmarks/stream-byte-budget/2026-10-21/README.md).
All three REVIEW.md passes completed with no remaining actionable finding in
this change. Local Go, native and Cluster race checks pass as recorded in
[validation](validation.json). CI and human review remain pending; no merge is
inferred. #406 implementation remains separate and in progress.

#410 implementation: [PR #426](https://github.com/alberttwong/redis-adbc-driver/pull/426).

2026-10-21 checkpoint: #410's PR #426 is merged and #410 is closed; #411 is
closed. #406 now has a pinned implementation (b40b88f), expanded tests (c064caa),
64 final exact native prefix/link comparisons and passing local Go/native/race
checks. The full dataset has recovered all 99,997,497 rows with all 105 fields,
but its fixed sweep remains active and the conditional benchmark window is not
ready. Complete backup and restore gates still apply. See the separate
[406 record](../aggregate-performance-406/intent.md); keep its issue open until
full-scale acceptance is met.

2026-10-22 pre-benchmark checkpoint: #406 PR #428 is merged by Albert and #406 is closed;
all ten CI checks passed. All three GitHub issues are closed. The full-scale
#406 before/after comparison remains outstanding and is continuing under the
already-authorized benchmark window after backup and restore validation.

Final completion: #406 full-scale acceptance now passes all 24 native trials
and eight profiles, with preserved evidence and verified release. #406, #410
and #411 are closed with merged implementations; no acceptance work remains
for these three performance issues. The separate owner API scan experiment
continues under its own fixed expiry.
