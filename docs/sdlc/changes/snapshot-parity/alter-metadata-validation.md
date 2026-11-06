# Definition-only ALTER avoids row copies and index rebuilds

Date: 2026-11-03. Parent 1d3f879. Protocol remains 15; no migration.
[Full feature and performance parity](plan.md) remains incomplete.

## Reproduction and implementation

The focused regression on 1,001 rows showed ALTER COLUMN v TYPE VARCHAR(8)
copying all 1,001 rows and creating one index even though v was VARCHAR(4) and
its stored values and Search encoding did not change. The baseline race test
failed on those observed command counts (0.31 seconds for the failing case).
This is evidence of unnecessary work, not a comparative throughput benchmark.

alterSnapshotTable now distinguishes definition-only changes after the normal
applyAlter/view-dependency checks. Sharing is allowed only with an actual action
list, no value conversion, added/dropped fields, new row-validation requirement,
rebound CHECK, dropped index or dropped sequence. Eligible changes preserve the
exact source prefixes, segment manifests, lineage and indexes. Conversions,
field changes, validations and explicit rekeying retain private row/index
construction. A nil action list is used by rekey and must never select sharing.

Both paths use the existing guarded descriptor pipeline. Snapshot hooks resolve
its metadata destination into a private COW object; the native-object graph
retains shared row-reference edges. Normal execution still seals outputs and
CAS-publishes the workspace after checking dependencies and both leases. No
committed descriptor is directly overwritten and no publication bypass or new
storage representation is introduced.

The new regression checks seven forms: VARCHAR widening, VARCHAR to TEXT, same
type without USING, SET DEFAULT, DROP DEFAULT, DROP NOT NULL and DROP CHECK.
All seven require zero row-image writes, zero new data identities and zero index
builds, with identical segment manifests. Fresh SQL sees the new defaults and
nullability; an old pin retains its original schema and values. A subsequent
real conversion still writes exactly 1,002 rows and builds exactly one index;
NOT NULL validation still rejects an actual NULL and leaves data unchanged.
A second regression stages a widening without publishing: public writes still
enforce the old limit. An intervening UPDATE causes 40001 at publication; the
rejected definition remains invisible. Explicit fresh ALTER/INSERT then succeeds,
and independent reference accounting passes after reclamation.

## Ordinary conversion coverage

Pinned RESP3 index inspection and predicate planning now use the same immutable
catalog as statements. Stored-value inspection resolves newest visible segments
rather than guessing the newest physical prefix. Rollback checks retain exact
published metadata comparison, drain private retirement, independently reconcile
row-reference counts and reject unreferenced data or indexes without registered
ownership. Released-generation checks inspect actual rows, index, directory and
graph ownership instead of obsolete mutable name registries. Exact index type
assertions include the managed deletion marker. The existing no-rewrite identity
assertions for VARCHAR widening and same-type ALTER remain in place and pass;
they were not relaxed to accept full copies.

Conversion expectations are unchanged: exact numeric rounding/overflow,
NaN/Infinity, NULL, strings/CHAR padding, bytea, dates/times/time zones, USING,
defaults, CHECKs, NOT NULL, view dependencies, Arrow/catalog schemas, indexed
filters and ordering. The concurrent conversion fixture now stages private rewrites and stale writes
around intervening SQL publications. It requires source-write, schema-change,
comment and DROP conflicts, preserves intervening values/metadata, and retains
pinned pre-conversion rows even after DROP. It does not force old readers or
independent writers to fail merely because a private rewrite exists. Legacy
mutable rewrite cancellation/close/lease/cursor hooks are not validated by this
subset and remain explicit work.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified with make -C go redis-up and the ordinary
Software wrapper. Mutating suites run sequentially on macOS ARM64. No ClickBench
run or reload. Operational logs and infrastructure details remain private.

- New metadata-only race regressions: two passed, zero skips/failures; root
  package 2.666 seconds. First sharing-only check after the change passed in
  0.69 seconds. Timing differences are not a throughput claim.
- Initial ordinary ALTER selection: 14 top-level passes, zero skips/failures;
  root package 21.740 seconds, before strengthened rollback orphan assertions.
- Final expanded ordinary selection: 15 top-level passes, zero skips/failures;
  root package 22.111 seconds. Includes strengthened rollback/orphan checks and
  deterministic conversion/write/comment/DROP conflicts. Earlier nine ALTER/
  constraint cases remain covered; the concurrent multi-action sample records
  seven commits and eleven conflicts.
- Complete snapshot race suite: 226 pass events, 15 explicit skips, zero failures;
  root package 295.458 seconds. Nine skipped cases require Cluster orchestration
  and six require actual permission principals. Those gaps remain open. The
  production implementation was final; later regression-only additions are
  covered by the final focused runs below.
- Final metadata-only race selection, including lineage identity, dropped CHECK
  enforcement and no publication bypass: two passed, zero skips/failures; root
  package 2.812 seconds before a final hook-ordering-only fixture adjustment.
  Recording hooks now install before starting a pin-renewal goroutine; the final
  reordered fixture passed both cases again in 2.838 seconds.
- Versioned native build completed. Rebuilt C/ADBC snapshot validation: 13 passed
  in 12.634 seconds. SQL/schema Arrow ABI: one passed in 0.40 seconds. IPC: nine
  passed in 1.12 seconds. TLS Ray: 17 passed in 13.22 seconds, three upstream
  warnings, no interface skips. These database-mutating checks ran sequentially.
- Final ordinary failfast run: 68 pass events, eight capability skips, one failure;
  root package 51.869 seconds. TestSQLAlterTypeCancel completes without cancellation
  because its legacy rewrite hook never runs in snapshot ALTER. This is not
  successful cancellation evidence or full-suite success. Snapshot-specific
  cancellation/close/lease/cursor fixtures still need completion.
- Unit-only packages: 919 pass events, 814 expected integration skips, zero
  failures. Expected skips do not prove server/platform compatibility.

Go formatting, local evidence links and git diff --check pass. Complete
ordinary-suite success, protocol-15 Cluster/platform/recovery,
complete ACL recipes, process resume, sustained compaction and broader performance
comparisons remain open. Self-review is not maintainer approval.


## Author review

Reviewed the sharing predicate against applyAlter's conversion/field/validation
flags, rekey's nil action list, view binding, immutable schema copies, shared
native-object edges, dirty-table index sealing, dependency CAS, lease admission
and orphan reclamation. The existing guarded descriptor pipeline and publication
entry point are unchanged. Both encoded-value and physical-index compatibility
must be proven before extending this path to further ALTER classes. This narrows
DDL row/index overhead; it does not prove sustained write/compaction throughput
or all-workload feature/performance parity.
