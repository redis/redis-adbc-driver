# Sparse parallel fixtures and segmented late projection

Date: 2026-11-03. Parent 4201d9c. Protocol remains 15; no migration.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction and implementation

TestSQLCheckedParallelInputs directly HSETs a published row to change its row-ID
attribute, then changes its sequence behind an already pinned plan. The snapshot
fence correctly refuses. Sparse fixtures now publish canonical destination keys,
registered directories, old-ID tombstones and native sequence bounds together
through a private workspace, then create plans. The helper rejects missing
sources, occupied/duplicate destinations and invalid bounds. It checks exact new
values/IDs and independently verifies unchanged pinned source rows and sequence.
Pure upper-bound adjustments publish only metadata. No published HASH or index
is mutated. The sparse 131,072-ID range contains only its intended few rows.

The fixtures retain checked arithmetic, NULL/error order, regex partial groups,
dependent grouping keys, spill passes, diagnostics, residual inputs and exact
serial/parallel comparisons. Four cases then pass before TestSQLParallelExactTopK
exposes an optimization gap: UPDATE creates multiple segments, and the planner
disables late projection despite the heap's existing snapshot-visible late fetch.
The initial restored selection fails that unchanged optimization assertion in
7.550 seconds for the root package.

Eligible segmented ordered scans now separate residual and output fields.
Visibility/predicates run per source; OFFSET/LIMIT and output materialization
run once after exact global order. The merge clears per-source late projection,
retains sort/predicate inputs and fetches only selected immutable physical images.
Its outer decoder receives output fields without reevaluating residuals. Parallel
top-K uses its existing snapshot-visible late fetch for heap entrants. Cap fallback
restores all output fields before removing late projection and index ordering.
Other eligibility guards remain. Storage, ACL, leases, publication, replay and
reference protocols are unchanged.

## Exactness and transfer coverage

A new 600-row fixture with 4 KiB payloads includes changed predicate membership,
changed output/NULLs and a tombstone. It checks ASC and eligible DESC NULLS LAST
global OFFSET/LIMIT, zero limit and offset beyond EOF. Source iterators must omit
payloads and have no local late limit. Actual payload HASH fetches equal selected
output count. Complete public SQL results match independent values. Reduced-cap
fallback preserves fields and exact rows. A parallel heap/eager comparison uses
the same pinned input and independent complete output, recording payload fetches.
This is a transfer comparison, not an elapsed-time or all-workload parity claim.

Initial new-test failures were setup errors: NULL needs explicit rendering, and
nullable DESC requires NULLS LAST for Redis's missing-last index sort. Both were
corrected without removing exact-value, global-limit or fetch-count assertions.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified through make -C go test-software. Mutating
suites are sequential on macOS ARM64. These are ordinary correctness fixtures;
no full-scale ClickBench load/run. Operational artifacts remain private.

Before the final parallel transfer assertion, the six-family race selection
passes 11 test events (six top-level and five ordered-limit children), zero
skips/failures; root package 11.088 seconds. Unit-only packages pass 923 events
with 822 expected integration skips and zero failures. Skips do not establish
compatibility. Final transfer/full snapshot and native results follow below.

The full snapshot race suite exits zero: 232 pass events, 21 explicit skips and
zero failures; root package 305.406 seconds. Skips are nine Cluster cases, six
permission cases and six isolated reader-model cases requiring their own gate.
They do not establish those capabilities. This run uses the final production
implementation and precedes the final test-only eager/late comparison addition.
Final unit packages also exit zero: 923 passes and 822 expected integration skips.

The first parallel transfer assertion observed eight late payload HASH lookup
requests versus 600 eager requests with identical complete results. A three-row
heap may probe each immutable segment to resolve its newest image; the original
one-request-per-row test assumption was incorrect. The corrected bound is heap
candidates times segment count, with a strict reduction against eager fetching.
Ordered merging uses known physical images and retains its exact selected-row
fetch count. This distinction preserves both the logical limit and the real
physical request cost; it does not claim eight materialized output rows.

Final focused race selection, including the corrected parallel transfer bound,
exits zero with 11 pass events and zero skips/failures. The observed comparison
is again eight versus 600 payload lookup requests, with both full outputs checked.
The versioned native build completes using make -C go build. All 14 C/ADBC
snapshot tests pass in 13.285 seconds with no skips, including the new segmented
ASC/DESC, offset, empty-limit, replacement and NULL case in both streamed and
materialized modes. Python syntax, Go formatting, evidence links and staged
diff whitespace checks pass.

## Review and remaining work

Author review covers canonical sparse construction, pinned metadata, newest
visibility before predicates, global limits, output/residual separation, cursor
cleanup, fallback projection and avoiding duplicate volatile predicate evaluation.
This is not independent maintainer approval. Full ordinary/platform/current
Cluster/failover/persistence checks, restricted reader ACL integration, process
resume, sustained compaction and broader performance comparisons remain open.
The pending protocol-16 reader design is not activated.
