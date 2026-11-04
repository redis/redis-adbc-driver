# Temporary object native lifecycle checkpoint

Date: 2026-11-04. Parent 895323a. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 129-name ordinary continuation passed TestSQLTemporaryObjectsOption,
then stopped at TestSQLTempTables' unpinned logical metadata inspection (root
1.446 s). The fixture now reads logical catalog existence through one pinned
manifest; physical row and liveness probes remain independent, so reclaimed
unreferenced storage can still be checked directly.

Temporary tables must use canonical native row/index identities. TRUNCATE must
allocate fresh storage, preserve the definition and support subsequent writes;
DROP, connection Close and expired-owner sweep must retire every captured segment.
The fixtures retain existing public SQL, owner isolation, shadowing, view and
schema-removal assertions. Private schemas must stay outside the public registry.

The expired-owner fixture now creates and publishes through executeSnapshot,
with a real session and snapshot-enabled store. Its heartbeat keeps objects alive
across three shortened 600 ms TTLs and another connection opening. After completed
statements, stopping that heartbeat without Close allows natural expiry; objects
remain until a new connection sweeps them, while the live owner's table survives.
This is an owner-liveness model, not an operating-system process kill or an
unread-input resume test. Cleanup stops the synthetic owner's heartbeat even on
failure and restores the global test TTL after owner cleanup.

## Validation and review

Owned strict-TLS Redis Software with noeviction and active effective 4x QPF
verified before testing. Seven lifecycle families pass with the race detector,
zero test skips/failures; root 9.080 s. The CUDA package has no applicable selected
tests and is skipped. An earlier six-family selection passed in 5.968 s before
the expired-owner fixture conversion. Mutating suites run sequentially.

Reviewed pinned logical reads versus raw physical/control probes, immutable
storage identity, complete segment reclamation, heartbeat cleanup ordering,
owner separation and retained public SQL assertions. No production, protocol or
ACL change. Author review does not constitute independent maintainer approval.
Full ordinary/platform checks, current-protocol Cluster recovery, restricted
readers, public input/process resumption, sustained compaction and comparative
performance remain open.

The next fresh 121-name ordinary continuation passed 16 test events, including
TempVisibility, TempIngest and TempDML, then failed TestSQLOrderByRoundedRuns'
optimization counters (root 21.831 s). It sorted 12 rows rather than using the
expected bounded rounded-value runs. This remains a performance-parity gate;
the continuation is not a complete ordinary-suite pass.
