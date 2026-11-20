# Specification

Date: 2026-11-20. Source base: `27e5a8d2`. Author: Codex. Issue: [#555](https://github.com/redis/redis-adbc-driver/issues/555).

`EvolveLiveHashes` / `live evolve` requires an exact current prefix/index, a full
ordered public column list, and explicit exclusive quiescence. Preview scans
source values without changing the registration. Existing source fields retain
their declared type and encoding; unselected fields remain application data.
A selected field must occur in at least one source HASH; empty source/schema and
SQL ALTER are outside the supported subset. Every supported change uses a fresh
index, including public SQL renames; there is no metadata-only optimization.

Selected source values are validated from atomic HGETALL images with strict
per-command and aggregate pipeline outcome checks. Full scan deduplication grows
with source cardinality; fetch pages are bounded. A durable `evolving` record
retains old/candidate identities before CREATE. New SQL fails during transitions;
existing readers retain coherent definitions or fail identity checks. Writers and
administrators must already be quiescent, including through recovery; independent
Redis clients cannot be fenced by this protocol.

A ready, exact-definition, failure-free candidate permits a CAS switch to
`retiring`; old index identity stays referenced until DROPINDEX without DD is
confirmed/observably complete. Resume never resends CREATE. Cancel before switch
moves to `aborting` and removes the observed candidate, preserving the old active
index. A direct native CREATE ACL refusal is recorded as proof permitting an
absent candidate cancellation. Unknown unobserved CREATE outcomes retain their
identity and block cancellation. Concurrent transitions fail CAS; uncertain
mutation replies never authorize implicit replay. Existing live storage format
records remain readable because optional transition fields are omitted normally.

See [intent](intent.md), [plan](plan.md), and [validation](validation.md).
