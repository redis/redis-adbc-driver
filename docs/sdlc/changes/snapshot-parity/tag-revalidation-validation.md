# Unknown TAG facts and immutable revalidation

Date: 2026-11-04. Parent 461cdc3. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 136-name ordinary continuation passed five TAG families, then stopped
at TestSQLTagOlderTables' unscoped logical metadata read (root 24.377 s). Its
helper previously deleted TAG fields and SET the committed logical descriptor.
It now publishes unknown indexed-string facts through a private current-format
metadata workspace, without changing row/index identity or old pinned metadata.
No earlier storage format is installed or migrated.

Native missing facts remain conservative. The fixture preserves exact comparisons
with the NOINDEX copy for padded/separated/control/Unicode strings, IN, grouping,
ordering and NUL-byte lengths. Unknown facts retain residual predicates and HASH
fallbacks. Native catalogs intentionally do not run the legacy in-place checker.
The fixture no longer waits for that mutable background update; an explicit
public ALTER COLUMN s TYPE VARCHAR USING s converts authoritative visible rows
into fresh storage and validates that new generation's facts.

Each passthrough rewrite checks every logical value before/after publication,
keeps the old reader's values and unknown facts immutable, requires known facts
on the new descriptor and a new physical layout, then releases the old pin and
requires its source rows/indexes/directories/reference records to retire. Exact
strings recover index aggregation; normalized and NUL-containing strings retain
the appropriate residual/authoritative behavior.

The writer race stages a real private passthrough conversion with exact input,
then publishes a padded INSERT. The old conversion must return 40001 without
changing the winning root; every private output/index must retire. The INSERT
raises the recorded level without falsely marking previously unknown values
checked. A new explicit ALTER request then validates both committed values;
there is no automatic SQL replay or metadata mutation under an old pin.

## Validation and review

Owned strict-TLS Redis Software, noeviction and effective active 4x QPF verified
before testing. Restored unknown-level and existing pure-model cases both pass
under the race detector, zero test skips/failures; root 3.781 s. The CUDA package
has no applicable selected tests and is skipped. Mutating selections run
sequentially. Compilation, formatting and diff whitespace checks pass. The earlier
five-family continuation remains separate evidence, not a complete ordinary pass.

Updated the TAG reference and implementation comment to distinguish conservative
native facts/fresh publication from the disabled legacy checker and to remove
claims that readers can observe another writer's newer string values. These are
documentation corrections, not a new background repair service or migration API.

Reviewed private descriptor/reference ownership, unchanged shared index/rows,
complete exact values and NULL/NUL behavior, safe optimizer facts before/after
validation, old-reader lifetime, stale-publication refusal, monotonic writer
requirements, no replay and private/source reclamation. No production behavior,
protocol or ACL changes. Author review is not independent maintainer approval.
Full ordinary/platform, current-protocol Cluster recovery, restricted-reader,
public input/process resumption, sustained compaction and comparative performance
remain open.
