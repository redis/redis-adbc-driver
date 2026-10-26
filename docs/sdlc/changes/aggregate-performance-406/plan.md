# Implementation and verification

See [intent](intent.md), [spec](spec.md). 2026-10-21; baseline e692a70.

1. Reproduce Q2's missing index union before changing production. The old
   matcher omits <>/!= entirely; SMALLINT versus INTEGER zero is not the cause.
   Focused before test fails with query=* and residual=true; after test passes
   exact counts, reversed operators, large bounds and missing defaults.
2. Screen scalar map reuse for Q4 and direct identity-list parsing for Q9/Q10.
   Keep authoritative HMGET and exact global tuple merge. Review found that
   expression callbacks could change page-level error precedence; restrict row
   reuse to direct column accumulators, preserving expression fallback/replay.
3. Run full Go/native/Cluster/race and focused differential/lifecycle tests.
   Profile pinned current builds outside timing, then repeat complete prefix
   queries with independently validated outputs and matched resources.
4. Coordinate the already-authorized conditional full-scale window with
   “Run ClickBench on c6a.4xlarge.” Do not time before its ready message and
   recovery/backup/restore gates. Deadline 2026-10-22T06:55:00Z leaves cleanup
   reserve before fixed resource expiry. Preserve results and release the window
   only with no active isolated jobs. Do not modify its recovery/fixed results.
5. Complete all three REVIEW.md passes, publish PR/evidence and close #406 only
   when its acceptance criteria are met; no CI/human approval or merge is inferred.

Initial scalar-map reuse (c7038b2) passes full Go/native/race checks and all 24
prefix results. Q2 median 0.248→0.017 seconds. Q4 client CPU/allocation volume
falls, but elapsed time is flat; Q9/Q10 parser CPU falls with small/no elapsed
gains. Preserve this screen. Next isolate direct typed accumulator vectors and
complete-fetch positional HMGET decode for scalar inputs, keeping command counts,
selection, batches and expression fallback unchanged. Reconfirm before promotion.

Q9/Q10 source experiment: the first client compactor used mixed Search/HASH
inputs; 24 exact prefix runs showed no elapsed gain despite lower server CPU.
Keep those raw results. The second version fetches all required fields in owned
HASH slices and halves prefix elapsed time in three paired trials. Promote only
with bounded 4096-entry compaction, unchanged weighted/global exact merge,
NULL/boundary/flush/cancellation tests and conservative per-primary latency
probes (warm connection then three samples below 2ms; 100ms socket timeout and
500ms context). Missing-default columns retain the pre-existing shape fallback.

Correctness review reproduced a new non-UTC TIMESTAMP <> TIMESTAMPTZ regression
(the shared comparison recognizer reached localBounds before the integer-only
guard; both operand orders returned 1 instead of 2). Move that guard before
constant evaluation/timezone planning. Keep before/after logs and regression;
repeat full verification on the corrected source. Add fractional/out-of-range
integer constant checks. This is an agent review finding, not human approval.

2026-10-22 follow-up: Albert merged PR #428 (439691c) and closed #406; all ten
CI checks passed on ad2faef. Continue the reserved full-scale comparison and
profiles before claiming acceptance. The bounded runner also journals its short
/bin/true isolation preflight before launch, so the owner can account for every
transient unit, including setup. This changes only the benchmark harness.

Full-window completion: 24/24 native trials and 8/8 profiles pass exactly on the
restored 99,997,497-row fixture. Verified S3 report/archive preservation and all
34 inactive services precede release at 05:52:01Z. The owner independently
verified the release and resumed separate API scans. Four owned local Redis
test containers were removed. See the full report and validation record.
