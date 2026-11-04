# Native UPDATE, DELETE and MERGE race checkpoint

Date: 2026-11-04. Parent fd7bbfd. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The preceding broad TRUNCATE selection passed 31 race events, then stopped at
TestSQLTruncateOrDropDuringUpdate/UPDATE/TRUNCATE_continue and its parent because
the legacy raceDrop hook never invoked the competing publication (root 45.057 s).
The native replacement/tombstone paths now invoke the existing test-only row hook
after acknowledged bounded image groups. With no test hook installed, execution
is unchanged. No lease, publication, index, ACL or replay behavior is modified.

The fixtures cover UPDATE, UPDATE FROM, MERGE UPDATE, MERGE INSERT, DELETE and
MERGE DELETE versus CONTINUE/RESTART TRUNCATE and DROP/recreation. Native UPDATE
and DELETE groups comprise bounded atomic image batches; the callback verifies
the exact registered directory count and first/last replacement values or
explicit tombstones. INSERT retains its own per-atomic-batch callback. DELETE
covers both 128-row input and 3,000-row input, replacing metadata-age hacks with
real acknowledged writes; even a small stale DELETE must refuse publication.

Every writer must return typed IO/40001, preserve the winning root and reclaim
private rows/indexes/directories/references. DROP/recreation installs an exact
sentinel that the refused writer cannot change. Subsequent explicit fresh SQL
must return every expected logical ID, row ID and NULL note. CONTINUE uses the
committed counter (including an empty MERGE INSERT target); RESTART starts at one.
No automatic SQL or input replay occurs.

A reader pinned before each race validates the complete old table and exact
old row identities/values after competing publication and fresh writes; the
source count stays intact. Old segments retire only after the reader releases.
The old mutable HASH/DEL, check-age and prefix-counter fixture helpers are removed
from this family.

## Validation and review

Run against owned strict-TLS Redis Software with noeviction and verified active
effective 4x QPF, with database-mutating selections sequential. The complete DML selection passes **26 race test events, zero test skips or
failures; root 59.624 s**. CUDA has no applicable selected tests and is skipped.
Formatting and diff whitespace checks pass. Full ordinary/platform/recovery, restricted
readers, public process/input resume, sustained compaction and comparative
performance remain open. Author review is not independent maintainer approval.

Reviewed private replacement and tombstone identities, acknowledged directory
counts, safe test hook reentrancy, typed conflict/root checks, complete old and
new rows, empty-target identity allocation, source preservation and reference-aware
retirement. The obsolete mutable raceDrop/error-string helpers have no remaining
callers and are removed. The unrelated legacy namesN helper remains for a pending
ACL fixture; this checkpoint makes no restricted-principal compatibility claim.

Unit-only packages with REDIS_URI and REDIS_SNAPSHOT_URI unset pass: **953 test
pass events, 866 expected integration skips, zero failures; root 26.630 s**.
These skips do not establish live Redis compatibility. The fresh final 27-name
ordinary continuation passes **45 test events, two explicit profile gate skips,
zero failures; root 77.504 s**. It includes complete wide HASH/Arrow fetch through
both APIs and worker/cancellation unit families. The gated worker concurrency and
mixed probes were not run; no throughput claim follows. A fresh complete ordinary
race run was started separately rather than combining continuation totals.
