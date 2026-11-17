# Specification

See [intent](intent.md), [plan](plan.md), [validation](validation.md).

On an owned Software fixture with verified 4x QPF/noeviction and trusted TLS,
compare ordinary execute and explicit executemany for INSERT/UPDATE/DELETE under
real restricted principals in both table modes. Independently prove the native
source-command refusal; require identical ADBC status/SQLSTATE and complete typed
logical state, plus unchanged unmanaged source HASHes. Never replay a mutation.

Hold a caller-owned parameter stream before row writes; cancel once while its
producer is entered, then let the foreign producer return. Require CANCELLED /
57014, one producer entry/yield, cursor reuse, explicit/repeated Close after join,
and exact unchanged state. This does not promise interruption of a foreign
callback that never returns, concurrent Close during execute, post-write
cancellation atomicity, full native leak accounting or latency improvement.

Keep all credentials, principal IDs and raw diagnostics private. Remove only
owned temporary schema/source/index/principals on normal failure/unwind; a
supervisor kill requires teardown of the complete owned fixture. No shipping
API or GC policy change; no adoption of the experimental Arrow dependency patch.
