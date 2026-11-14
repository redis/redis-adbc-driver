# Validation and decision

Measured clean source `255e58919e7ad2327752a07cd30380968aad6798`, actual clean
baseline `88774f10cf16fc4260e6ad5077097f4de8828406`. Keep candidate unapplied.
All 24 construction and 112 native samples are retained; no source tuning,
selective mode guard or discarded sample followed timing.

Corrected unit and race each passed 21 top-level / 65 including subtests, zero
skips/failures. Verified Software passed all ten requested top-level / 33 including
subtests, zero skips/failures, including managed/unmanaged 105-column retained
ownership/NULLs and finish/release/cancel. Two earlier endpoint-free selections
accidentally included live cases: Go exit zero, four endpoint-missing skips,
controller exit one. Corrected exact pure regex retained strict zero-skip checks.
A separate unsupported CLI version command exited two; build metadata and native
GetInfo verify versions. Every attempt remains in the evidence.

Isolated wide construction improved 35.5–36.7%, with candidate binding setup
shared by both arms. Actual native unmanaged medians improved 2.6–6.4%, but
managed cells were mostly 1.4–3.0% slower; consistent benefit in both modes was
required in the original contract. RSS median differences were −0.54%..+0.44%,
all ranges overlap. No timing-to-memory tradeoff or post-hoc eligibility change
justifies overriding the gate. Production Go files remain baseline-identical;
exact candidate/test patch is preserved.

Parent and compaction sibling performed source-only agent review before timing,
no blockers. This is not human maintainer approval. Every fixture loan returned
empty/index-free and QPF/TLS/RESP3 verified, all clients/processes closed. Shared
infrastructure/credential cleanup belongs to the parent and is pending.
See the [complete report](../../../../benchmarks/table-modes/direct-arrow-2026-11-13/README.md),
[intent](intent.md), [contract](spec.md) and [plan](plan.md).
