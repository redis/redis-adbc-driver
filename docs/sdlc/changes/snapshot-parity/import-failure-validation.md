# Import failures retain reports and protect committed data

Date: 2026-11-03. Parent source `8f81f98`. Storage remains protocol 14;
[full feature and performance parity](plan.md) remains incomplete.

## Reproduction and correction

A late private-index error discarded the completed managed-import report.
The new TestAdoptIndexFailure actually executes FT.CREATE on the Software server
and then injects an error into its reply path. Both initial import and explicit
refresh reproduced a nil report before the fix. The production correction returns
the existing HashAdoption plan alongside the error; Applied stays false. It does
not suppress the error, replay SQL/input/index creation or publish partial rows.

The regression requires exactly one index creation attempt. After the failure,
the catalog root is unchanged; a new destination remains absent and an existing
copy retains all five original rows/keys and its sum. Reference-aware reclamation
must remove the aborted private index. Every application HASH and the application
index remain unchanged. A separate explicit invocation can publish the complete
copy, including source changes made before a failed refresh. This is an explicit
new operation, not automatic retry after an uncertain result.

The old test used an in-place-adoption progress message that managed import no
longer emits, so it never exercised its fault. Its replacement tests a real
server-side private index followed by a late client-observed failure, for both
create and refresh. This does not simulate every Search indexing error or prove
recovery from every network/server fault.

## Compatibility tests and ownership

TestAdoptBlockers now requires blocked Apply to return its complete report and an
error, with an unchanged published root and no destination descriptor. Native
Boolean encoding misfits, ignoring application __rowid fields, rejection of
control-storage prefixes and exclusion of nested-only collections remain covered.
The old broad public-prefix expectation belonged to in-place storage; current
managed schema-prefix isolation is covered by TestAdoptedPrefixOverlap.

The previous v0.0.11 upgrade and unfinished-adoption resume tests fabricated
legacy metadata inside an already initialized current catalog. They are replaced
by TestSnapshotImportRejectsLegacyStorage in the disposable snapshot suite. It
starts with actual legacy metadata or an unfinished legacy reservation and no
snapshot catalog. Two explicit import attempts and ordinary SQL Open must reject
without initializing, upgrading, resuming, deleting or changing that storage.
Legacy metadata bytes and every source HASH are independently checked. These
checks implement the agreed no-migration scope. Resumable current large operations
remain required; legacy rejection is not evidence that they are implemented.

## Verification

Owned Redis Software fixture: Search, strict TLS, active effective 4x QPF and
noeviction reverified using make -C go redis-up. Database suites run sequentially.

- Before fix: both initial-import and refresh late-index cases failed because
  their completed report was nil; the injected fault was exercised.
- Corrected focused report/blocker/late-index race run: four top-level cases and
  two fault subcases passed, no skips/failures, 3.085 seconds package time.
- Focused legacy-rejection race run: both fixture subcases and parent passed,
  no skips/failures, 1.890 seconds package time. Final checks below also verify
  metadata-inspection commands themselves return success.

- Full current snapshot race suite: 141 pass events, seven expected Cluster-only
  skips, zero failures, 135.505 seconds. This run preceded only the extra checks
  of DUMP command errors in the legacy test; the final focused run below covers
  those checks.
- Final legacy-rejection race run with checked metadata-inspection replies:
  both subcases and parent passed, no skips/failures, 1.933 seconds package time.
- Unit-only `go test -json -count=1 -p 1 ./...`: 902 pass events, 716 expected
  integration skips, zero failures. Skips are not compatibility evidence.
- Native shared library rebuilt using `make -C go build` with version metadata.

- Rebuilt native C/ADBC snapshot validation: 13 passed, no skips/failures,
  14.374 seconds.
- Final ordinary adoption/report/failure race run, including strict one-attempt
  and every-source-HASH checks: five top-level cases and two fault subcases
  passed, no skips/failures, 4.279 seconds package time.
- CLI TestAdoptCommand race run: passed, no skips/failures, 2.071 seconds.
- Changed Go formatting, local Markdown links and git diff --check passed.

IPC/Ray were not rerun for this report-return correction; their prior published
results belong to the source-key checkpoint. No general matrix or performance
claim is made for this error-reporting correction.

## Author review and remaining work

Reviewed failure propagation, report ownership/Applied status, atomic publication,
non-replayed index creation, orphan-index reference reclamation, external ownership
and legacy initialization rejection. Self-review is not maintainer approval.
Remaining keyed-write/application-index/sweep/ACL families, current large-operation
resumption, sustained compaction, platform/Cluster recovery and comparative
benchmarks remain open in the active goal.
