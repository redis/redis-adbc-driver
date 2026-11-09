# Retained operation validation

Date: 2026-11-09. Issue: [#335](https://github.com/redis/redis-adbc-driver/issues/335).
Author: Codex. Status: local validation complete; delivery CI is tracked in
[PR #355](https://github.com/redis/redis-adbc-driver/pull/355).

Implementation started at main `cc2d6601`; implementation commits `93e2a8f3` and
`db7b4324` contain the tested APIs and regressions. Earlier recovery runs used
the working tree leading to those commits; final focused checks were repeated
on `db7b4324`. Documentation-only delivery records do not change the tested code.
Follow-up `87a496a5` removes an unnecessary ordinary-publication lease check;
the retained-operation path is unchanged. A subsequent reproduced regression,
`TestSnapshotPublicationOutcomeLookupIsReadOnly`, showed expired-pin pruning in
the shared outcome path. Moving outcome lookup ahead of cleanup made that lookup
strictly read-only; thirteen publication/conflict/rebase tests passed with race
detection after the fix. Final CI must identify the PR head.
Historical saved-branch results are not evidence
for this implementation.

## Environments and observed results

The owned Software Cluster fixture used image `redislabs/redis:8.2.0-78.18`,
reporting Redis **8.6.2**, with three primaries and three replicas across three
Software nodes. Each orchestration verified Cluster API, strict TLS, noeviction,
active scaling factor four and effective Search worker/routing configuration.
AOF used appendfsync-always. The separate RDB run deliberately changed the owned
database to a verified frequent snapshot policy; it is not an AOF result relabeled
as RDB. Infrastructure, endpoints, credentials, certificates and raw logs remain
private. The ordinary single-endpoint Software fixture was started and verified
with `make -C go redis-up`; it is not counted as a Cluster API deployment.

- Twenty focused Cluster tests passed, zero skips: five capture-completion/row-ID
  tests and fifteen operation tests. These cover native copy/adoption/foreign
  refresh, SQL/defaults/dependency conflicts, Arrow exact types/NULL/binary values,
  separate-process partial output and sealed indexes, stale/competing owners,
  incomplete input/runtime markers, plan integrity, lost replies, terminal outcomes
  after manifest GC, record capacity, read-only status, ALTER and temporary-target
  refusal.
- An additional separate-process test passed after terminating the client at an
  acknowledged publication, before its API could receive or resolve the reply.
  The next process observed the original committed count without republishing.
- The killed-owner regression used actual process termination after an acknowledged
  partial output batch. Takeover refused the live lease, then succeeded after the
  real two-minute lease elapsed. An independently retained two-minute operation
  expired, lost its TTL plan, and became reclaimable without publishing.
- Real metadata-slot primary promotion passed. Captured input and an independently
  sealed SQL operation resumed in new processes after verified role/Search/QPF
  recovery. Exact results and the metadata reference audit passed.
- An all-node kill/restart with AOF passed the same recovery assertions.
- An all-node RDB restart passed, including checkpoint writes on every routed
  primary and completed snapshots strictly later than acknowledged probes.
- Three native C/ADBC tests passed through the built versioned shared library:
  SQL and Arrow preparation followed by separate Python-process continuation;
  and conflict SQLSTATE/handle preservation followed by explicit abort.
- The broader `^TestSnapshot` selection passed with race detection on the verified
  single-endpoint Software fixture in 423.458 seconds. Environment-gated Cluster,
  recovery-orchestration and optional profile/ACL tests are not counted as
  backend coverage by this run; the dedicated Cluster checks above supply their
  separate evidence.
- All six Go packages passed a unit-only run with race detection. Backend-gated
  skips in that run are expected and are not backend compatibility evidence.

The first promotion scenario intentionally prepared two new table creations but
incorrectly expected them to be independent. The second publication correctly
returned `40001` because both depended on the same schema table-list metadata.
The corrected scenario prepares an INSERT into an existing independent table.
This preserves conflict checking; it does not suppress or retry a failed statement.

## Boundaries

Tests prove complete capture and completed/sealed index checkpoints. A runtime or
process interruption inside native index mutations is explicitly abort-only and
must never repeat uncertain FT.CREATE/DROPINDEX/notifications. Unread input and
incomplete capture cannot detach. Temporary targets remain session-owned. The
optional CopyHashes public key column remains UTF-8 text; binary source keys and
provenance are verified through the native capture/adoption paths.

The 128-record admission test includes terminal records; ordinary manifest/pin
limits still apply sooner to active operations. No full-scale throughput or
performance-parity claim is made from these small correctness fixtures. Default
synchronous writes do not acquire retained-operation records or durable per-command
operation charges. Checkpoint plans are bounded to 16 MiB, with a cumulative
64 MiB control budget, and have an atomic TTL. Publication installs the terminal
outcome in the same catalog HSET as the new root.

See [spec](spec.md), [plan](plan.md) and [public contract](../../../snapshots.md).
