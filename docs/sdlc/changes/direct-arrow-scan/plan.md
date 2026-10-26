# Work and verification

Date: 2026-10-21. Status: local work complete; PR/human review pending.
See [intent](intent.md), [spec](spec.md).

1. Add shared Arrow builder buffering and reuse projection decoding scratch.
2. Preserve regular small-result row counts and metadata checks, generic paths,
   partition lifecycle, configured batch sizes and consumer buffer ownership.
3. Add exact parity, error/allocator, retained-buffer and allocation benchmarks.
4. Run Go tests with a clean standalone server; focused race tests on Cluster;
   native public ADBC validation for both topologies.
5. Build pinned baseline/candidate; interleave wide/narrow regular/partition
   trials and native redis-py references with independent exact validation.
6. Record evidence and review all three REVIEW.md passes; open a PR for review.

## Evidence and outcome

[Validation results](validation.json) record the final full Go suite and
race-enabled focused tests on standalone and Cluster. Native validation of the
exact benchmark library passed on both: 371 passed, eight expected unsupported
capability skips and three subtests passed per topology. No new skip was added.
No Cloud or Redis Software validation was run for this change; the project's CI
matrix and human review remain pending.

All 96 wide/narrow public-API/native-reference executions passed independent
exact-value and multiplicity checks. See [benchmark report](../../../../benchmarks/wide-transfer/direct-arrow-2026-10-21/README.md)
for raw trials, source and binary identities. Wide peak worker RSS falls 87.7%
regular / 57.0% partition; elapsed medians are effectively flat. Regular wide
client CPU rises 11.7%, partition 2.3%. Narrow elapsed ranges overlap without a
material regression. The synthetic 105-column decode-to-Arrow diagnostic
allocates 64.1% fewer bytes; it is not whole-query allocation evidence.

## Review record

Agent self-review on 2026-10-21; this is not human approval:

1. Correctness/lifecycle: reviewed guarded eligibility, required-but-unselected
   decode errors, skipped/LIMIT-tail validation, fitting/coercion, batch sizing,
   sparse range transitions, owner attachment, known small-result counts and
   final metadata checks. Tests cover null/defaults, aliases, duplicates, >2^53
   integers, NUL strings, timestamps, partial builder failure, cancellation and
   retained output after early release. The focused tests exposed an empty
   result representation change, corrected before final verification. A test
   initially used a connection-level batch option; it was corrected to the
   supported database option, without adding skips or relaxing assertions.
2. Security/ownership: no storage mutation, ACL/TLS, namespace, or external HASH
   ownership behavior changed. Unsupported/adopted/foreign plans retain guards.
   Arrow output owns its buffers; no borrowed Redis-memory optimization.
3. Intent/evidence: production sources and benchmark binary/runner hashes were
   verified against the recorded commits. Memory improvement and CPU tradeoff
   are explicit; no speed, WAN, production or 100M-row inference. The runner's
   post-measurement change only makes hiredis metadata conditional on native
   reference runs; measured worker/trial code is unchanged.

No unresolved actionable finding from these three passes. A human review and
CI decision are still required before treating the PR as approved/released.
