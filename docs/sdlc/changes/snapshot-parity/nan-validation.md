# NaN visibility, physical coverage and reclamation fixtures

Date: 2026-11-04. Parent 95ef3ac. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction and correction

The ordinary diagnostic after length metadata reaches five pass events and one
failure at TestSQLNaNIssue110, root package 2.720 seconds. All four numeric/index
variants report three HASHes still present immediately after DROP. Immutable
snapshot generations are reclaimed after reference retirement, not synchronously
deleted during catalog publication.

The issue-110 script retains all original SQL/value assertions and now checks
complete reference-safe reclamation of every captured segment, including its
HASHes, index, exact directory and ownership records. NaN flag inspection uses
the existing pinned descriptor helper. The no-orphans helper validates real
FT.INFO/directory counts, independently counts physical HASHes for every segment,
then compares SQL COUNT(*) with an authoritative directory merge that resolves
newest versions and filters tombstones before counting. A newest-prefix count
cannot establish this property for a segmented table.

The deletion fixture checks five fresh visible rows, validates physical coverage
and proves the original two NaNs remain readable under the old pin. After pin
release, TRUNCATE and DROP reclaim every retired segment. The old fixture that
changed a published HASH to an earlier NaN encoding is replaced with a uniquely
owned application HASH containing literal NaN. CopyHashes captures it using an
explicit native type, publishes canonical indexed infinity encoding, and supports
NaN predicate reads and UPDATE. DROP/GC reclaim its managed copy while preserving
the exact original application value. This tests the agreed managed-copy contract;
it does not add a migration or permit mutable published storage.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primary shards, Search,
noeviction and effective active 4x QPF verified before database selections.
Mutating suites run sequentially on macOS ARM64. Logs, endpoint identifiers and
operational artifacts remain private. No ClickBench run or reload.

The intermediate directory-count assertion exposed a fixture mistake: nextKeys
includes winning tombstones. The corrected helper uses next, which fetches the
winning images and filters deletion markers, before comparing visible counts.
The subsequent diagnostic passes 19 events before the old mutable NaN repair
fixture fails. That fixture leaves an invalid published index/image count; the
owned disposable database is reset through the snapshot harness before the
corrected ownership case is run. No invalid published data is repaired in place.
The corrected ownership case passes under the race detector with zero skips or
failures, root package 2.725 seconds. The reset selection also reruns and passes
the retained late-projection case, root package 1.972 seconds.

The complete `^TestSQLNaN` race selection (without fail-fast) passes 21 events,
zero skips/failures, root package 39.005 seconds. Unit packages pass 946 events
with 844 expected integration skips, zero failures, root package 26.644 seconds.
Unit skips do not prove database compatibility. Formatting, evidence links and
diff whitespace checks pass.
This checkpoint changes regression fixtures only. The production snapshot/native
results remain those of the [ingest checkpoint](ingest-admission-validation.md).
Full ordinary/platform/Cluster/recovery, process resume, sustained compaction
and comparative performance gates remain open.

Author review inspected immutable source ownership, pins, physical versus visible
row counts, tombstone filtering, exact NaN/Infinity behavior, source preservation,
reclamation and original SQL assertions. This is not maintainer approval.

The next ordinary diagnostic selects 672 top-level names after TestSQLNaNNumeric.
It reaches five pass events and one failure at TestSQLNarrowExactScan, root
package 1.086 seconds: its internal store reads metadata without a statement
pin. Later selected cases did not run. This is not a full ordinary suite and
disjoint diagnostic counts are not added to claim one.
