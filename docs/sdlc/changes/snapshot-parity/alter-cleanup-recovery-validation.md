# Interrupted ALTER retirement and connection recovery

Date: 2026-11-04. Parent 8d9ff48. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 391-name ordinary continuation passes 81 test events and records 220
explicit gated skips before TestSQLAlterCleanupResumes fails on an unpinned
legacy catalog read (root 25.056 s). Later selected cases did not run. Snapshot
endpoint, Cluster API, reader-role and other explicit fixture gates remain
unproved by their skips.

The cleanup fixture now creates all fields through SQL and uses a real DROP
COLUMN publication. An old pin must retain every exact id/value/dropped-field
image across the ALTER, while current output contains no dropped field. It no
longer inserts arbitrary fields into published HASHes or installs mutable
RetiredFields/PendingCleanup metadata and legacy cleanup markers.

The driver's worker is closed before the old pin releases, leaving one explicit
collector to perform the first bounded batch. A lost reply is injected only
after Redis has deleted 256 of 600 retired rows. The durable directory must show
344 remaining rows, with the exact boundary HASHes absent/present. Opening a fresh
connection must finish that retirement before the test calls any explicit
collector again. Reference-aware checks then require all retired rows, indexes,
directories and ownership records gone. Complete current id/value rows remain
unchanged. Re-adding the same SQL column yields NULL for every existing row;
a fresh UPDATE cannot reveal any old dropped value.

The shared GC batch observer now filters script row keys by its selected physical
prefix, as its direct-DEL branch already did. This prevents unrelated retired
generations from triggering the target's interruption or row-ID checks.

## Validation and review

Owned strict-TLS Redis Software, noeviction and effective active 4x QPF verified
before each selection. Mutating selections run sequentially. The restored ordinary
ALTER cleanup case passes under the race detector, zero test skips/failures;
root 2.787 s. The initial port's recovery assertions completed but teardown tried
to use its intentionally closed connection (root 1.797 s); final table cleanup
now uses the reopened connection. Compilation, formatting and diff whitespace
checks pass. The existing gated lost-reply GC batch case also passes under the race detector,
zero test skips/failures; root 1.827 s. It verifies a fresh collector
resumes exactly at row 257 and removes the remaining 344 in two bounded batches.
The CUDA package has no applicable selected tests and is skipped.

Reviewed immutable published input, reader lifetime, real SQL field removal,
confirmed versus uncertain deletion accounting, exact remaining-directory progress,
automatic new-connection recovery, cleanup ownership, column-name reuse and
source-specific observer hooks. No production, protocol or ACL changes. Author
review is not independent maintainer approval. This is retirement recovery, not a
public API for resuming SQL or unread input. Full ordinary/platform, current-
protocol Cluster recovery, restricted-reader, sustained compaction and comparative
performance gates remain open.
