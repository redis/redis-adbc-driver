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

The complete actual Software SQL RBAC race selection also passes, root
24.833 seconds. It exercises shared-role boundaries, stable grants, captured
payload denial, poisoning, mixed-producer global capacity, stale-directory
refusals, original-token lost-reply resolution and cancellation. It does not
broaden the existing role/ACL trust boundary or raise retention quotas.

Fewer directory commands are a performance hypothesis. Complete native reads,
writes and matched comparative timings remain required; unit command counts do
not establish query latency or sustained throughput parity. Keep this candidate
separate from the currently validated protocol-16 repair/hosted CI evidence.
