# Visible balanced partitions and current-main default prefetch

Date: 2026-11-04. Snapshot parent fcd80ad; incoming main b1d1bf0,
merged as e527433. Production remains protocol 15. Full [parity](plan.md)
remains incomplete.

## Reproductions and implementation

The ordinary diagnostic stops at TestBalancedPartitionReads because its readiness
fixture reads metadata without a pin. Replacing only that setup with real pinned
descriptor/index validation exposes a production failure: partition zero returns
zero rows instead of 625. Planning counts the latest segment's tombstones rather
than the complete visible generation.

Balanced planning now counts each proven visible segment query under the producer
pin, excluding newer replacements and tombstones. All segment count requests
share the existing 256-request limit. The two-second internal deadline includes
visibility proof construction after readiness/cap discovery. Exhausted budgets
discard partial quantiles and retain complete equal-width coverage; unavailable
bounded masks also retain that coverage. Parent cancellation and actual metadata,
permission or transport failures remain explicit. Readiness is checked for every
counted segment before returning the cuts. No descriptor format or membership
pruning is introduced.

Immutable exact directory cardinality and canonical endpoints can prove a fully
contiguous run. Such a run is clipped to each older segment's bounds and becomes
one exclusion interval without enumerating its IDs. Proven runs and bounded
scattered ID masks merge into at most 256 ranges. The 4096 scattered-ID limit,
exact Search ID boundary, deletion filters and fallback remain. This improves
optimized reads after bulk changes as well as partition weighting.

A new aggregate fixture initially observes 6001 HASH image reads despite valid
contiguous masks: fully masked empty segments omit non-NULL reducer fields, so
the existing exact reducer falls back. A proven zero COUNT now supplies zero
non-NULL counts without parsing absent fields; COUNT(expr) is zero and other
supported reducers retain their typed NULLs. Nonempty malformed replies still
fall back. The corrected query returns the same exact count/sum with zero HASH
image reads and zero mask-ID-list reads. This is a transfer/path observation,
not an elapsed-time or whole-workload parity claim.

Incoming main enables wide prefetch by default with a build-only scanprefetchoff
baseline. Its Search-cursor-only eligibility check accidentally suppresses snapshot
directory/candidate prefetch. Their immutable physical row counts now prove
single-page cases can remain synchronous; larger sources retain bounded lookahead.
Once a worker starts, the consumer never inspects its mutable discovery state.
Worker ownership, cancellation joins, consumer exact-ID/tombstone checks and the
incoming consumer EOF readiness recheck remain. The incoming EOF/drop primitive
uses a separately owned mutable index and a real pin, preserving its I/O assertion
without dropping a published managed index. Historical mainline prefetch timing
artifacts remain labeled as such; they do not validate this combined revision.

## Regression scope and local evidence

Owned strict-TLS Redis Software, Redis 8.6.2, three primary shards, Search,
noeviction and effective active 4x QPF verified before database selections.
Mutating suites run sequentially on macOS ARM64. Logs, credentials and operational
identifiers remain private. No ClickBench run or reload.

The balanced readiness-only reproduction fails in root package 6.717 seconds.
The original balance/coverage/budget selection then passes eight race events,
zero skips/failures, root package 11.878 seconds. The expanded balanced/contiguous/
scattered-mask selection passes 12 events, zero skips/failures, root package
16.143 seconds. It verifies four 625-row descriptors retain exact 22500..24999
membership after producer close, further DML, TRUNCATE, DROP and GC; independent
reset capabilities share one committed generation and both are explicitly released.
Cancellation retains its specific status. A six-segment sparse allocation with
128 requested cuts must abandon partial quantiles under a shared request/time
budget, preserving equal-width coverage without fetching row images or mask IDs.
The first version requested only 32 cuts and legitimately completed in budget;
that was a test assumption, not a production failure.

The contiguous-mask fixture validates 5000 tombstones over 6001 input rows, exact
remaining count 1001 and sum 5506501, then overlapping/adjacent replacement and
deletion runs with independently expected groups. Its old pin keeps the original
count/sum; no row images or mask-ID lists are fetched. Existing scattered-mask
fallback still prevents resurrection.

Before prefetch adaptation, the merged three-case selection passes one event and
fails four events: the unpinned EOF fixture and directory/candidate variants that
never start their worker. Final prefetch selection passes six events, zero skips/
failures, root package 6.584 seconds. The snapshot variants now have 4097 rows so
lookahead is meaningful and check every exact value/NULL through DML, DROP, GC
and final reclamation. The complete public 5000-row SQL/partition wide test passes,
zero skips/failures, root package 89.912 seconds, including actual default-worker
selection, exact values, range fallbacks, lifecycle and mutation retention.

Current default unit packages pass 946 events with 849 expected integration skips,
zero failures, root package 26.916 seconds. Unit skips do not prove database
compatibility. The versioned native library rebuilds with make -C go build.
The complete snapshot race selection (`^TestSnapshot`, `-race -count=1 -p 1`)
passes 256 events, 21 explicit skips and zero failures, root package 311.726
seconds. Skips are nine Cluster API cases, six permission cases and six separately
gated reader-model cases, not compatibility passes. Current scanprefetchoff unit
packages pass 946 events with 849 expected integration skips and zero failures,
root package 27.498 seconds. Rebuilt-native C/ADBC snapshot validation passes
all 18 tests in 18.362 seconds, zero skips/failures. New native cases fully fetch
balanced producer-close/drop partitions (250 exact rows per descriptor, integers
beyond 2^53 and NULLs, release rejection) and all 64 exact columns of a multi-page
stream after DML/TRUNCATE/DROP. Full ordinary/platform/Cluster/recovery, explicit process
resume, sustained compaction and comparative workload performance remain open.

## Author review

Reviewed canonical/unique directory proof assumptions, clipped interval unions,
scattered-mask budgets, zero-row reducer semantics, per-segment count admission,
parent cancellation, immutable worker ownership, output identity/NULL checks,
capability release and external primitive isolation. No SQL/input replay, storage
revision, migration, multi-statement transaction or ACL change is introduced.
This is not independent maintainer approval.

The next ordinary diagnostic uses a fresh current root-test listing and selects
659 top-level names after TestBalancedPartitionReads. It reaches seven passes,
one explicit snapshot-endpoint skip and two fail events (one child and its
parent) at TestPartitionUnsupportedLayoutOwnership/adopted, root package 0.958
seconds. The fixture reads descriptor JSON without a statement pin; later
selected cases did not run. This is not a full ordinary suite, and disjoint
selections are not added to claim one.
