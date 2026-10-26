# Plan and evidence

2026-10-22. Status: draft; no approval inferred.

Existing local changes were preserved and inspected. Fixed retained-reader
transport cleanup and corrected HASH scan statistics to retain the approximate
flag under concurrent writes. Added blocked-read cancellation checks for RESP2/3
with read timeouts disabled, pool isolation and non-replay assertions.

Redis Software 8.6.2, one shard: make -C go redis-up verified active QPF 4x.
Focused Go statistics/retained execution/SQL cancellation/transport tests passed
with REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed after management-plane
noeviction verification. Focused blocked-read interruption checks passed.

Pending: full Go/native suites, race/stream/union lifecycle coverage, Cluster/TLS
fault tests, temporary-source close races and updated public documentation.
Atomic snapshot statistics and source snapshots remain unresolved. Draft PR
must not close #476, #477 or #478. See [intent](intent.md) and [spec](spec.md).

## Merge resolution

Merged main at 29284b3. Kept authoritative HASH counting and incorporated the
upstream statistics ACL denial assertions from #493. The new interruption
fixture now uses RESP3, matching #489. Reverified Software 8.6.2 with active
4x QPF. Focused TestStatistics, TestRetainedExecution, TestSQLCancel,
TestTransport and TestExecutionTransport checks passed after resolution.

## Follow-up after merge

PR #492 merged. The remaining transport lifecycle fixes, current public contracts
and final validation are recorded in
[the lifecycle completion record](../adbc-lifecycle-completion/plan.md).
The draft status above describes the earlier review stage, not current PR state.
