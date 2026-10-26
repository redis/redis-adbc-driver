# Wide Arrow transfer investigation

Date: 2026-10-22. Requestor: alberttwong. Author: Codex.
Base: `aab9612`. Status: investigation complete; review pending.
Issue: [#191](https://github.com/redis/redis-adbc-driver/issues/191).

Wide readers reconstruct typed Arrow columns from Search and per-row HASH
replies. Existing Redis-only experiments do not measure the gap to PostgreSQL
or ClickHouse on an equivalent result. Establish that comparison, retain
independent value/multiplicity checks, and screen bounded client improvements.

Consumers are regular and partition ADBC readers. Preserve exact stored values,
NULL/default behavior, cancellation, ownership, Cluster routing and fallback.
Do not equate reduced allocation with improved elapsed time. A new server
module, storage format, public protocol or production deployment is outside
this implementation; document its tradeoffs before choosing an architecture.

See [specification](spec.md) and [plan](plan.md). No human approval is inferred.
