# Implementation plan

Date/source/requestor/status: see [intent](intent.md) and [spec](spec.md).

1. Inspect current issue bodies, bridge, metadata resolver and existing profilers.
2. Draft #399 ownership/deletion contract; do not enable replacement.
3. Reuse public DBAPI no-result execution in a separate explicit scenario wrapper;
   add strict same-binary ABBA auditing and tests before collecting measurements.
4. Inventory #446 owners, correct paired BindStream reference ownership and
   migrate internal callers; reproduce safety gates without GC changes.
5. Collect current #444/#445 foreground observations on verified Software; choose
   any production optimization only with passing correctness/performance gates.
6. Run focused tests, review ownership/error paths, and record actual evidence,
   failures, skips and unresolved work in [validation](validation.md).

No issue closure or successful optimization is inferred from adding a harness.


Implementation/check work is recorded in [validation](validation.md). #399's
proposal deliverable is complete. #444/#445 retain open performance work; #446
retains blocked lifetime/sanitizer gates; #447 has complete exploratory windows
with an inconclusive causal result. No automatic issue closure is selected.
