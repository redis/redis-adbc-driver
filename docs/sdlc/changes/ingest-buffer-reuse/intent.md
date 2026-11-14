# Reuse bounded Arrow ingestion conversion storage

November 13, 2026. Requested by the user as a performance follow-up. Baseline
`88774f10`; implementation and validation pending, no approval or release claimed.

Arrow ingestion currently allocates a row-view slice and flat Value matrix for
each conversion slice. Reuse execution-local storage after synchronous insertion
finishes, without retaining prior string/decimal/record values or input buffers.
Do not change publication, source ownership, input consumption or write recovery.

See [contract](spec.md), [plan](plan.md) and [validation](validation.md).

Outcome: evidence only. The original frozen contract remains above; measured
source `55396058` is preserved unapplied. Native memory gains accompany mixed
elapsed outcomes, so the original adoption gate is not established. Production
Go remains identical to `88774f10`. See [validation](validation.md).

Publication: [PR #429](https://github.com/redis/redis-adbc-driver/pull/429) is open
as evidence only. No merge, release or human maintainer approval is claimed.
