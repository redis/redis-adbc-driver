# Ordinary aggregate parity and compact partition bounds

Date: 2026-11-03. Parent f7f09a6. Protocol remains 15; no migration.
[Full parity](plan.md) remains incomplete.

## Reproduction and changes

The broad ordinary-suite inventory included aggregation helpers that constructed
unscoped legacy stores. Isolated FILTER, CHECK metadata and exact DISTINCT cases
first reproduced missing-table/metadata errors. Inspection now pins immutable
catalog metadata using RESP3, retains the generation for low-level plans and
preserves cancellation contexts. The SQL connection keeps its independent
statement context; later SQL must see its own current generation. Hooks are
installed before clients are shared, with scope injection preceding catalog
admission and key resolution.

Large pagination fixtures now seed through public SQL rather than inserting
unregistered HASHes into published storage. Cursor assertions normalize RESP3
maps and still require an explicit zero open-cursor count. Corrupt-definition
and noncanonical-value regressions stage registered private metadata/images and
publish through the normal protocol. They preserve pinned source values.
Inspection field counts merge newest visible images instead of counting only
the latest segment's physical prefix.

Snapshot ALTER materializes defaults. Tests now assert exact materialized values
and retain the virtual-default eligibility guard on copied planner descriptors
with both Missing and MissingThrough set. No guard, error, cancellation, overflow
or exact-result assertion is skipped. The wide fixture uses one bulk SQL insert;
this is setup for tuple/spill correctness, not a sustained-compaction benchmark.

### Actual tuple execution defects exposed by restored fixtures

The 65-column, 10,000-row integer tuple case includes NULLs, signed extremes,
adjacent integers above 2^53, replacements and tombstones. Independently invoked
server partitions initially queried only the newest segment because visibility
preparation lived solely in the coordinator. They returned empty/zero groups.
Visibility preparation is now shared: coordinators prepare once before workers;
an unprepared partition prepares a local copy rather than mutating a shared shape.
This hardens the standalone executor; the public coordinator already prepared
server visibility before this change.

The compact tuple scan also ignored partition bounds on snapshot overlays. Two
disjoint partitions each read the full visible input, doubling weighted COUNT and
SUM. An empty interval could incorrectly read the full table and report integer
overflow. The compact scan now passes exact row-ID bounds into the authoritative
snapshot scan. Tail ranges use the full int64 upper bound consistently in both
compact and ordinary row fallbacks. The restored low-level compact reproduction
exercises the same scan used when the public coordinator selects that strategy;
it does not claim a separate end-to-end public-query timing reproduction.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries behind the proxy,
Search, noeviction and active effective 4x QPF verified with make -C go redis-up.
Database-mutating suites run sequentially on macOS ARM64. Operational artifacts,
credentials and infrastructure identifiers remain private.

- Initial isolated helper reproduction: all three selected cases failed before
  fixture changes. Their missing-table/metadata and virtual-default assumptions
  were corrected without weakening semantic coverage.
- Restored aggregate set: nine top-level tests plus three nested tuple cases,
  **12 pass events, zero skips/failures**, 30.271 seconds. This run preceded the
  final additional tail-layout and pinned-source assertions.
- 25,001-row aggregate checks independently calculate counts, exact sums,
  distinct values and averages; preserve global ordering/LIMIT/OFFSET, bounded
  pages, cancellation, consumer errors and cursor cleanup.
- 24,006-row DISTINCT checks preserve 12,003 exact values, group counts,
  cancellation after cursor creation, binary-capability fallback, collision
  rejection, noncanonical spelling fallback and cursor cleanup. Duplicates span
  separate row-ID ranges; managed generations intentionally share one catalog
  slot, so this is not evidence of managed-row distribution across shards.
- Pinned inspection regression retains exact values above 2^53 after a later
  UPDATE and DROP and rejects attempts to mutate that pinned generation.
- Unit-only packages: 919 pass events, 811 expected integration skips, zero
  failures. Later changes add fixture assertions without changing the production
  implementation covered by this unit run.
- Final ordinary aggregate race selection, including both finite/tail layouts
  and unchanged pinned sources after noncanonical fixture publication: 12 pass
  events, zero skips/failures, 32.962 seconds. Both strategies require available
  immutable visibility; the compact branch must use its scalar projection path.
- Complete snapshot race suite: 224 pass events, 14 explicit Cluster/permission
  skips, zero failures, 299.226 seconds. This does not close those skipped cases.
- Versioned native build completed. C/ADBC snapshot tests: 13 passed, 14.551
  seconds; SQL schema/Arrow ABI case: one passed, 0.48 seconds; IPC: nine passed,
  1.25 seconds; TLS Ray: 17 passed, 15.70 seconds, three upstream warnings and no
  interface skips. Go formatting, local evidence links and git diff --check pass.

## Hosted CI and remaining work

The existing f7f09a6 hosted Test run completed: helpers and coverage passed;
Linux AMD64 and ARM64 Software Cluster jobs failed in the ordinary Go suite.
The [broader failure inventory](main-merge-failure-inventory.json) remains visible;
this checkpoint does not establish that the full ordinary suite is green.

A subsequent make -C go test-software run with -json -failfast reverified
Software/QPF and supplied the retained-control declaration through the supported
wrapper. It stopped at TestACLRekeyTakeoverReleased: its legacy unscoped move
fixture is rejected with "snapshot catalog access requires a statement snapshot."
This isolates a remaining fixture/lifecycle adaptation without running later
cases into admission-limit cascades; it is not a successful full-suite run.

Remaining ordinary fixtures, legacy move/corruption assumptions, schema-restricted
ACL/utility coverage, complete protocol-15 Cluster/platform/recovery checks,
explicit process resume, sustained compaction measurements and broader performance
comparisons remain open. No product throughput improvement is claimed from
changing fixture setup or fixing doubled aggregate counts. No ClickBench run or
reload was performed. Self-review is not maintainer approval.
