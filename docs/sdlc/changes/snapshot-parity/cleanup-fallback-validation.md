# Complete snapshot fallback input and retained capture

Date: 2026-11-04. Parent f10842c. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction and correction

The pending-cleanup fixture originally fails in unscoped sharedScanPlan setup.
Planning with a pin and a cloned cleanup descriptor exposes a production error:
Search ordering remains enabled, and the allocated-ID fallback reads only the
latest physical prefix. After segmented DELETE it returns zero rows instead of
the required 2395 visible rows. The restored fixture reproduces both errors
before the production correction.

Snapshot HASH/cap/cleanup fallbacks now scan exact immutable row directories.
Newest replacements and tombstones resolve before predicates; partition bounds
seek those directories exactly. This includes unchanged older segments and
avoids enumerating allocated-ID holes. Non-snapshot primitive behavior remains.
The cleanup planner requests complete input with the full residual and required
ordering columns, leaving explicit ordering, OFFSET and LIMIT global. Existing
aggregate cleanup guards still prevent unsafe Search reductions.

Retained capture separately rejects the same native layout solely because its
descriptor has a cleanup marker. The regression reproduces this Not Implemented
error after live partition reads succeed. Native snapshot capture can now use
its exact immutable directories with that marker; other source-layout ownership,
temporary/moving/adoption checks remain. No protocol, ABI, quota or permission
change is introduced.

## Regression guarantees

The complete-input fixture retains 2505 source rows and deletes 100, checking
every one of the 2395 matching visible identities without duplicates. One row
moves to physical ID 9007199254741999, above exact Search double precision.
An exact singleton directory bound finds its SQL value, global ORDER/OFFSET/
LIMIT still produces 14,15,16, cancellation stops after one callback and all
pinned pages survive a later TRUNCATE and GC. Its 15-second context bounds a
regressed allocated-ID loop; it is not an elapsed-time benchmark.

Aggregate fixtures preserve filtered/unfiltered counts and exact sums with zero
FT.AGGREGATE attempts. Hooks count actual Search calls without replacing index
metadata. Cleanup flags are cloned for planner tests or published as private
metadata values, never written over a pinned descriptor. Existing live broad/
direct descriptors retain their old generation, while three new partitions
produced under the marker preserve exact, nonoverlapping membership. Retained
capture also succeeds. All survive flag clearing, TRUNCATE, DROP and GC with
exact 0..299 values; the finite descriptor retains value 0. Tests explicitly
release live capabilities and retained results.

The legacy truncate-readiness primitive test uses attributes already present,
so it issues only readiness reads on the immutable index, never FT.ALTER. Its
timeout/pending/retry assertions remain. Public snapshot sealing is covered by
the [readiness checkpoint](alter-readiness-validation.md).

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and effective active 4x QPF verified before mutating selections. Suites run
sequentially on macOS ARM64. An initial verification command used the wrong
management port and failed before Go tests started; the owned fixture was
restored and its effective configuration reverified before subsequent tests.
Operational artifacts and raw logs remain private. No ClickBench run/reload.

Sparse cleanup/partition selection: two race passes, zero skips/failures, root
package 5.927 seconds. Combined cleanup/ALTER/range/terminal/truncate readiness:
20 race pass events, zero skips/failures, root package 9.685 seconds. Unit
packages: 945 pass events, 836 expected integration skips, zero failures. Skips
and parent passes whose children skip are not database compatibility evidence.
The versioned native library builds with make -C go build. Full snapshot race
selection (`^TestSnapshot`, `-race -count=1 -p 1`) exits zero: 244 pass events,
21 explicit skips, zero failures; root package 301.449 seconds. Skips are nine
Cluster API cases, six permission cases and six separately gated reader-model
cases, not compatibility passes. Rebuilt-native C/ADBC snapshot validation passes
all 14 tests in 13.459 seconds, zero skips/failures. Database-mutating selections
run sequentially. Formatting, evidence links and diff whitespace checks pass.

Hosted parent f10842c has terminal failures in
[Test run 41800886780](https://github.com/redis/redis-adbc-driver/actions/runs/41800886780)
and [Release binaries run 41800886883](https://github.com/redis/redis-adbc-driver/actions/runs/41800886883).
Both Linux Test jobs failed at Go tests; release Linux jobs failed at Software
tests and the macOS job failed at Test. Helpers, coverage, distribution and
Ubuntu native DuckDB checks passed. The separate
[DuckDB extension run](https://github.com/redis/redis-adbc-driver/actions/runs/41800886704)
also passed. Later dependent release/upload/wheel jobs skipped; no release or
complete platform success is claimed. These runs predate the current patch.

Private terminal macOS logs include the old cleanup/truncate fixtures, ACL cases,
TestSQLEmptyReadsNothing and TestParallelBulkIngest among failed function names,
followed by the global ten-minute package timeout. These names are not independent
product-defect counts. The macOS server distribution/version and isolated timeout
cause were not verified by this inspection. Local Software results are not
attributed to that runner, nor is its timeout a comparative performance result.

The next ordered diagnostic selects the 753 top-level names after
TestTruncateAttributeReadinessRetry in the earlier test listing. It reaches
55 passes, zero skips and one failure, root package 1.564 seconds, stopping at
TestParallelBulkIngest. Three of its four writers receive explicit 40001 conflicts;
one commits, leaving the seed plus 2000 rows (2001 distinct rows). This does not
establish loss of acknowledged rows. The fixture's all-writers-succeed expectation
remains unmet; concurrent input admission/commit behavior needs separate work
without replaying consumed Arrow input. Later selected cases did not run. This
is not a full ordinary run, and separate selections are not summed into a green
suite claim.

## Author review and remaining work

Reviewed visibility before predicates, exact sparse bounds, complete/global
ordering and limits, cancellation and callback errors, pin retention through DDL,
native versus external capture guards, real metadata validation, no source/index
mutation, explicit result release and unchanged format/limits. This is not
independent maintainer approval. Full ordinary, restricted reader ACL, current
Cluster/platform/recovery, explicit process resume, sustained compaction and
broader comparative benchmarks remain open.
