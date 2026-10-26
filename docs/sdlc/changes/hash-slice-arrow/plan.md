# Direct-slice plan

2026-10-21. See [intent](intent.md), [spec](spec.md).

1. Screen preboxed HMGET field names and a bounded argument arena: no clear
   overall speed win in the first three-round screens; do not promote alone.
2. Screen direct HMGET slice decoding, then optional argument reuse in isolation.
3. Implement the smallest winning path; share pipeline queue/execute handling
   without changing generic map semantics. Add focused semantic/ownership tests.
4. Confirm the exact production binary with five interleaved wide/native trials,
   a narrow control and profiles outside timing. Validate full Go/native suites,
   race checks and Cluster. Preserve skips and raw failures/outliers.
5. Review correctness/lifecycle, security/ownership and intent/evidence; clean
   task resources and publish a follow-up PR. Do not merge #419 without user
   authorization or infer CI/human approval.

Completed: direct positional decoding is the only promoted experiment. Five-round
wide/native confirmation improves regular time 9.7% and partition time 10.4%;
peak worker memory also falls. Initial narrow partition timing was higher; a
six-round balanced repeat is effectively flat. All 164 benchmark executions
validate every value/multiplicity. Three-scan allocation bytes fall 30.1%.

Current-main full Go: 1,015 pass events, 15 skips, no failures; Cluster RESP2/RESP3
focused race checks pass. Native ADBC: 372 pass, 8 skips, 3 subtests per deployment.
Current main's trusted TLS/ACL/resharding harness passes the exact library.
See [validation](validation.json) for commands, skip names and limits.

All three REVIEW.md passes are complete: no remaining actionable correctness,
lifecycle, ownership/security or evidence finding in scope. Generic paths,
missing-value semantics and parent readiness/owner gates are preserved. The
remaining native four-reader gap and imperfect profiling attribution are explicit.

While working, #419 and independent TLS readiness fix #420 merged. The follow-up
was rebased onto current main; alternate readiness experiments were retained on
a separate local backup branch rather than mixed into this PR. An automatic
approval rejection prevented optional obsolete remote branch deletion; it remains
intact and does not block this work. No human approval or merge is inferred.

Implementation: [PR #422](https://github.com/alberttwong/redis-adbc-driver/pull/422).
