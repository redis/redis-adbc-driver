# Native rekey process-kill recovery

Date: 2026-11-04. Parent 22186f9. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 507-name ordinary continuation stopped at TestSQLRekeyKilled on an
unpinned legacy catalog read (root 3.211 s); subsequent cases did not run.
The crash fixture now runs the actual public forced-rekey statement in a separate
test-binary process and uses two explicit production boundaries. At the second
source page, its first 256-row private batch must be durably acknowledged and its
output directory exact. At the committed publication reply, the root and new
physical layout must already be public. The child blocks at the boundary, and
the parent kills and reaps it without executing connection Close or private abort.
Durable reader/work control must still exist after process death.

Three 50,000-row cases cover private-copy recovery by a connection opened and
immediately closed, private-copy recovery by an already-open connection's next
write, and preservation of a committed rename after coordinator death. Each
compares every old pinned row and native integer type after recovery. Private
output remains unpublished; a new explicit rename after rollback must use fresh
storage and reproduce every exact row, including acknowledged subsequent writes.
Committed output remains queryable under its new binding, the old binding is
absent, old data is reclaimed only after reader release, and fresh writes work.

Owner expiry is advanced atomically on existing control records only after the
OS confirms the owner is dead. No published input, metadata or index is edited.
This proves process-death cleanup/publication semantics; it does not measure the
natural two-minute lease duration or expose an API to resume unread input or SQL.
Child stderr and infrastructure details remain private.

## Validation and review

Owned strict-TLS Redis Software with noeviction and verified effective active
4x QPF. Mutating checks are sequential. Initial native rewrite: three subcases
and parent pass under the race detector, zero test skips/failures; root 41.614 s.
Final ownership/type assertions also pass all four race test events, zero test
skips/failures; root 41.138 s. The CUDA package has no applicable selected tests
and is skipped. Formatting and diff whitespace checks pass.

Reviewed actual acknowledged server boundaries, durable ownership after abrupt
exit, root visibility, exact complete results, client-request versus driver replay,
private non-reuse, reader retention and reference-aware reclamation. No production
code, protocol or ACL changes. Author review is not independent maintainer approval.
Full ordinary/platform, Cluster API/failover/persistence recovery, restricted-reader,
public process/input resumption, sustained compaction and comparative performance
gates remain open.
