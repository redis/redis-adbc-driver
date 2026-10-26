# Specification

2026-10-21. Author: Codex; requestor: Albert Wong. Status: implemented.
Base: `9510b9d`. Refs #359/#360/#362/#363.

The [public runtime contract](../../../retained-partition-results.md) defines
per-operator SQL behavior, exact source layouts, options, defaults/hard limits,
authorization, ordering, resource/cost model and failure/lifetime semantics.
The earlier [design](../../../partition-execution-design.md) predates enabling.

Use isolated source pools with pre-decode RESP admission; cumulative shared
transport/source/execution allocation and work credits; bounded final IPC;
namespace output reservations before capture; one-minute coordinator deadline.
Source reads never write/adopt/migrate application HASHes. Validate dependency,
index, topology and observable cardinality changes before atomic publication.
Execute admitted global operators once with the ordinary SQL evaluator on
captured rows, then split final output by ordinal chunk groups.

Public version-2 descriptors contain only namespace/job/manifest/expiry and
chunk interval. Reader-only bootstrap skips source catalog recovery and SQL;
independent credentials authorize bounded GETRANGE/TIME reads. Preserve fixed
TTL, nonce abort, explicit namespace-scoped DROP PARTITION RESULT, immutable
replay, bounded orphans and no SQL re-execution. Persistent quota ledgers require
noeviction/volatile policies or an explicitly verified operator guarantee.

Acceptance: exact Arrow values/schema for every added query class; source byte
keys/ownership/legacy/large IDs; malicious RESP/IPC limits and budget failures;
independent process/ACL/TLS; cancellation, early release, coordinator/worker death,
expiry/corruption/eviction/revoke and quota races; full configured native tests,
retained/live regressions and scoped performance with complete fetch/oracles.
Record platform/Cloud gaps rather than closing #363 on local evidence.

See [intent](intent.md), [plan](plan.md), [evidence](evidence.md).
