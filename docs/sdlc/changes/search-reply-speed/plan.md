# Work and verification

2026-10-21, implementation and local verification complete; PR review pending.
See [intent](intent.md), [spec](spec.md).

1. Profile current RESP3 scan CPU and allocation cost on the existing fixture.
2. Remove redundant field-map normalization, with compatibility tests including
   malformed replies, duplicate/unsupported fields, list reducers and ownership.
3. Profile the candidate and run parser microbenchmarks.
4. Run full Go, standalone/Cluster race lifecycle checks, and native validation.
5. Interleave baseline/candidate and RESP3 native references on the wide/narrow
   fixture. Gate success on measured elapsed time; retain identities/raw trials.
6. Complete REVIEW.md's three passes, publish evidence and open a PR.

## Evidence and outcome

[Benchmark report](../../../../benchmarks/wide-transfer/search-reply-2026-10-21/README.md)
contains raw CPU/allocation profiles, protocol-matched wide/narrow native
comparisons and a separate ADBC confirmation. Exact source/binary/runner hashes
are checked against recorded commits. All 120 executions in completed matrices
passed independent full-value and multiplicity validation; an earlier harness
index-name failure is retained separately, not hidden or counted as complete.

Regular elapsed time improves 7.1% in the first comparison and 7.2% in the
confirmation (separated confirmation timing ranges). Partition medians improve
4.0% initially but only 0.6% in confirmation: no reliable partition wall-time
gain is claimed. Client CPU falls about 14.5% regular and 9–12% partition.
Narrow elapsed medians increase 1.5% / 2.8%, with overlapping ranges; no narrow
speed claim. RESP3 is explicitly requested for both clients and observed on
the benchmark control connection. RESP2 is an explicit compatibility option.

[Validation](validation.json): full RESP3 standalone Go suite passes across all
three packages; native validation of the exact benchmark library passes with
RESP3 standalone/Cluster and RESP2 standalone (371 passed, eight expected
unsupported-capability skips, three subtests passed on each). Focused race checks
cover both topologies. No assertion was weakened or new capability skip added;
the new opt-in read-only profiling test skips outside its explicit fixture run.
Redis Software, Cloud and the CI architecture matrix are not locally rerun.

## Review record

Agent self-review, 2026-10-21; not human approval:

1. Correctness/lifecycle: verified RESP2/RESP3 map/list compatibility, malformed
   row/cursor errors, duplicate-field final-unsupported-value deletion, numeric
   formatting, missing values, typed empty maps and list-length alias fallback.
   Byte-value mutation tests verify owned strings. Cursor cleanup and downstream
   SQL/Arrow/readiness machinery are unchanged and exercised through both APIs.
2. Security/ownership: no SQL generation, ACL/TLS, namespace mutation, external
   HASH ownership or borrowed-buffer behavior changed. New parsing only copies
   supported response fields and retains old validation behavior.
3. Intent/evidence: elapsed time, rather than allocations alone, is the success
   criterion. A second wide run was added because the first contained a slow
   round. Partition inconclusiveness and small higher narrow medians are explicit.
   The initial profiling loader's RESP3 FT.INFO shape error and subsequent native
   hardcoded-index warm-up failure were harness defects, corrected and recorded.
   The protocol correction preserves the existing RESP3 driver default.

No unresolved actionable finding from these passes. CI and human review remain
pending; no release or cross-deployment performance claim is made.
