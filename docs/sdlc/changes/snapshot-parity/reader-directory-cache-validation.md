# Reader directory hints

Status: isolated candidate; not integrated or performance-validated.

Admission and reclamation currently read the complete bounded reader namespace
directory in a separate read-only script before sending the control command.
Stable membership repeats this round trip on every non-point control operation.
The candidate keeps an immutable, atomic process-local list per store. It is a
routing hint: the server must still prove exact directory cardinality, every
canonical namespace's membership and uniqueness, and lease HASH resource limits
before mutation. The server also checks the directory's byte bound on every
admission, including with a warm hint. Equal cardinality alone cannot authorize
a namespace replacement.

Only a direct native `reader namespace changed before mutation` reply permits
refresh and another attempt, within the existing eight-attempt bound. Plain,
wrapped, joined and unknown transport errors stop; cancellation stops refresh.
Cold discovery errors cannot send an admission. Unknown acquisition outcomes
continue to resolve the original token read-only. Point validation, renewal and
release retain their direct paths. Domain registration discovery is unchanged.
No limits, storage format, lease lifetime or authorization scope change.

Focused unit checks pass with the race detector, including stable administrator
and restricted-reader admission, refusal classification, cancellation, failed
discovery and bounded churn. New Software regressions cover repeated stable
admission, equal-cardinality replacement, removal, partial-pin exclusion, and
directory/HASH byte limits after warming the hint. The native race selection
passes all five test events, no skips/failures, root 1.805 seconds, on owned
Software 8.8 with strict TLS, noeviction and API-verified effective 4x QPF.
A first private preflight was invoked from the wrong directory; the complete
rerun verifies configuration before testing and passes. No failed preflight is
counted as policy verification.

The complete actual Software SQL RBAC race selection also passes: 21 test
events, no skips/failures, root 24.833 seconds. It exercises shared-role boundaries, stable grants, captured
payload denial, poisoning, mixed-producer global capacity, stale-directory
refusals, original-token lost-reply resolution and cancellation. It does not
broaden the existing role/ACL trust boundary or raise retention quotas.

At candidate source `c306768`, the versioned library with SHA-256
`2460b91dee76f7e7d85e823bd779268890ac72a27d52554da67bf86e2ea78653`
passes complete native ADBC validation: **565 tests and 3 subtests**, **8
capability skips**, **81.50 seconds**. The environment is Python 3.14.10,
PyArrow 26.0.1, manager 1.13.0, pytest 9.2.0 and official validation revision
`47a5ddf3f2a617ac88da3891848e000c74d55b0e`. There is no selection override.
The complete snapshot race selection then passes **528 test events**, **29
explicit skips**, root **374.564 seconds**; all six test-bearing packages pass,
with no failures or data races. QPF/TLS/noeviction are reverified before each
phase. Suite durations are validation evidence, not performance comparisons.

The preserved comparison reference is production source `4f77d9c`, library
SHA-256 `74ec9ffe708e1feeeab4adff9481733a39d4ab4568baf9c0160269949b42c76c`.
Production differences from that reference are limited to the three cache
implementation files. The older native cap-response correction is present in
both libraries; test/workflow repairs remain separate from the candidate.

Fewer directory commands are a performance hypothesis. Complete native reads,
writes and matched comparative timings remain required; unit command counts do
not establish query latency or sustained throughput parity. Keep this candidate
separate from the currently validated protocol-16 repair/hosted CI evidence.
