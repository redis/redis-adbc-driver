# Validation and review

Date: November 13, 2026 (America/Los_Angeles). Author/reviewer: Codex.
[Intent](intent.md), [specification](spec.md), [plan](plan.md). Human review pending.

Focused protocol and lifecycle checks pass with the race detector:

```sh
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=4 go test -race -count=1 -p 1 \
  -timeout=10m -run 'TestExecutionPool|TestExecutionTransport|TestConnectionCloseWaitsForExecutionCleanup|TestHashPrefetch|TestSnapshotReaperRunning' .
```

The new cases cover drained pool reuse, distinct execution lifetimes, active
owner isolation, stale cancellation during the next lease, timeout identity,
missing-value/native replies, uncertain command replies without replay, native
first pipeline errors hiding later I/O failure, the one-idle-pool bound and
concurrent completion/Connection.Close. Existing child cancellation, blocked
socket, prefetch joining and cleanup-before-maintenance-close regressions pass.

A coordinator-owned Redis Software fixture independently verified active/effective
4× QPF, six Search workers, mnp/32, four-GiB noeviction RAM and strict data
TLS/RESP3 before focused live checks. TestManagedSingleGenerationDirectoryScan,
TestUnmanagedLiveSourceWrites (adopt/foreign), and the opt-in
TestOLTPStatementReadPoolProfile passed; every returned Arrow value was fully
fetched and independently checked. Harness cleanup closed all clients and
removed its fixture data/indexes. Operational logs/identities stay private.

The instrumented Go profile compared forced-cold and reusable execution pools
with the same candidate source/workload. Each cold operation opened two sockets
and issued four HELLO commands; after warmup, reusable operations opened zero
sockets and issued zero HELLO commands. The observed managed median was about
2.85 ms versus 6.80 ms cold; unmanaged about 3.00 ms versus 6.80 ms cold. These are
small, instrumented client-call diagnostics, with possible concurrent compilation,
not native end-to-end performance claims. That diagnostic run preceded the final
pipeline-uncertainty guard and duplicate-unmanaged-FT.INFO removal; those results
are not relabeled as final-code measurements.

The new TestUnmanagedStatementChecksIndexOncePerFreshPin verifies one index
inspection per prepared execution, live large-int source changes and failure
following external index-definition replacement. Its live execution and final
profile/native measurements are assigned to the coordinator's subsequent verified
integration slot and must be recorded before final performance claims.

Review: exclusive leases and detach/interruption share the pool mutex, cached
pools cannot be inserted after closure, all uncertain pipeline replies taint the
pool, maintenance retains separate clients, and no snapshot/row proof is cached
across pins. Existing direct-key execution already resolves known managed row IDs
newest-to-oldest/tombstones and preserves unmanaged membership. Four lease-valid
round trips remain in the read profile; they are not removed by a time-based
lease cache. Reducing them requires a separately proven atomic read protocol.
No production server compatibility or storage-format change is introduced.

Follow-up lifecycle hardening: both command and pipeline hooks refuse a completed
execution context after pool transfer, before sending anything. Active command
counts participate in the completion check, so unfinished command machinery
cannot be lent to another execution. New protocol regressions verify no stale
command/pipeline reaches the server and no in-flight command pool is cached.
The complete focused race selection above passes again after these guards.
