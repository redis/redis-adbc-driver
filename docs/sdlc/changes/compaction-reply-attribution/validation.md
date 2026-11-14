# Validation and decision

November 13, 2026. Final scope is evidence only, with exact diagnostic patches
archived unapplied. All 269 non-test Go/module files and ordinary tests remain
byte-identical to baseline `88774f10`. No production performance change is proposed.

Focused race tests: 17 passed on `549cbd4a`; 18 passed on corrected `d2abe735`,
zero failures/skips. Both dense diagnostic jobs and the unchanged dense control
finished full independent 20K+20K row checks. The initial instrumented Go pass
was rejected by complete-sample audit: all 634 flags invalid, with observer-induced
existing bounded release retries. A separately frozen correction records errors
without changing release outcomes and fails only the final attribution assertion.
That corrected job had 317 successful single-attempt releases but zero valid
per-release counter pairs, so the live diagnostic test exited1 as expected.
All original attempts and raw-log digests remain in results.json; no rerun hides
or replaces a failed attempt. This is not a claim that the corrected live test passed.

The new control reproduced 6.254 s copy-release hook time / 6.247 s TLS Read. Corrected
monitor requests moved much of the wait into the preceding INFO request; cumulative
counters gave 316 zero deltas and one 20-call jump. These do not establish per-release
server execution, CPU, queue or network contributions. The exact prior clocks,
observer correction and pacing/order limitations are in the
[report](../../../../benchmarks/table-modes/compaction-reply-2026-11-13/report.md).

Parent source review of initial `549cbd4a` found no blockers; parent explicitly
requested/approved the record-only observer design before the corrected run.
This is agent review, not human approval. Parent final source/evidence review of `3d304257` plus the documentation follow-up
found no blockers; the parent independently reran the 1,268-observation audit.
This remains agent review, not human approval.
The public audit recomputes all sample/counter/timing arithmetic and exact attempt
coverage without Redis. No production code was cherry-picked from the experiment.

Exact patch SHA-256 and `git apply --check` passed for both archived variants.
Relative file links and a scoped exact-secret/private-path/URI scan passed with
zero matches. Whitespace checks pass outside the exact patch artifacts: their
context lines retain leading diff spaces before Go tabs/blank lines, which Git's
outer-diff whitespace check flags. Those bytes are intentionally preserved with
their hashes rather than rewritten after measurement.

All loans ended with clients/processes closed, empty/index-free DB and effective
Software 4x QPF, strict TLS/RESP3 reverified. Root owns final shared infrastructure
teardown; it is pending sibling experiments. Operational artifacts/IDs, credentials,
license and TLS material remain private. No ClickBench or alternative provider.

See [intent](intent.md), [spec](spec.md), [plan](plan.md) and [results](results.json).
