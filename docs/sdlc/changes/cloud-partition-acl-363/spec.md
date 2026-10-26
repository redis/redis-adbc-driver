# Contract

Date: 2026-10-21. See [intent](intent.md) and [plan](plan.md).

Cloud principals use a dedicated ACL rule and role each, scoped to exactly one
owned Pro database. API operations verify subscription/database names, active
state, RAM, TLS and a single-database subscription before mutations. Register
fresh principal names before POST so partial failures can be reconciled without
adopting pre-existing names. Never replay POST, log response bodies or propagate
API keys to workers. Poll asynchronous tasks and actual data-plane permissions.

Preserve all existing OSS/Software assertions. Cloud planning may use 137 only
after verifying the real server cap is at least that lower bound. Native TLS
must reject an unrelated CA and a trusted certificate with a wrong hostname.
Cleanup principals even on failure; report cleanup errors. The resource owner
must delete the disposable database/subscription after validation.
