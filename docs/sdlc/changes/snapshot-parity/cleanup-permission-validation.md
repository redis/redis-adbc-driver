# Snapshot cleanup permission recovery and unmanaged collision regression

Date: 2026-11-03. Parent c187c75. Storage remains protocol 15; no migration.
[Full feature and performance parity](plan.md) remains incomplete.

## Regression reconciliation

The ordinary Software suite stopped at TestACLRekeyTakeoverReleased before
reaching its ACL setup: its unscoped legacy move fixture was rejected by snapshot
catalog admission. Mutable rename jobs are replaced by private snapshot outputs;
restoring that old metadata protocol would contradict the no-migration contract.

TestSQLSnapshotCleanupDeniedRecovery now preserves the applicable lifecycle
requirement: a failed collector must leave private rows intact and allow another
collector to finish immediately, without waiting for the failed owner's lease.
It builds and retires a real registered private generation with a Search index.
A deterministic FT.DROPINDEX denial must occur exactly once, leave all three
rows, their directory and index intact, and report zero deletions. A fresh
collector must immediately reclaim the same prefix, report exactly three deleted
rows and remove its index, directory and ownership record. A committed row above
2^53 stays pinned and unchanged across cleanup; subsequent public SQL writes work.
The ordinary case runs without requiring protocol ACL SETUSER.

TestSnapshotCleanupPermissionRecovery separately exercises the same checks with
an actual Software API-managed principal denying FT.DROPINDEX. Authentication,
directory reads and FT.INFO are positive controls; only NOPERM on the exact
private index establishes the expected denial. This is a cleanup regression,
not proof of the complete restricted-reader/writer command recipes.

The next ordinary failure was TestAdoptInsertCollisionAtomic. Its unmanaged
HASH primitive fixture had inherited rawClient's snapshot hooks, so its legacy
prefix reservation was correctly rejected without a statement scope. The test
now clones a native single-endpoint or Cluster client without driver hooks for
this isolated primitive. No SQL descriptor is fabricated. Both writers still
pass preflight before racing; exactly one insert succeeds, the other reports an
existing-key collision, and the winning HASH has exactly one field. Driver SQL
and managed-copy fixtures retain snapshot admission.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF reverified through make -C go test-software. Mutating
suites ran sequentially on macOS ARM64. No ClickBench run or reload.

- Focused ordinary race selection: both cleanup recovery and unmanaged HASH
  collision passed, zero skips/failures (0.28 and 0.03 seconds per case).
- scripts/snapshot_gc_acl.py: real server-side cleanup denial/recovery and all
  existing row-batch permission preflights passed under the race detector:
  five top-level cases plus two import subcases, zero skips/failures; 2.152
  seconds for the Go package. The script verified QPF/noeviction and removed its
  temporary Software principals and associated API resources. Later script edits
  change only descriptive text, not principal rules or execution.
- Unit-only packages: 919 pass events, 812 expected integration skips, zero
  failures. This run preceded the native-client fixture adjustment, which the
  subsequent focused race run compiled and exercised.
- One intermediate make invocation lost regex dollar signs during expansion and
  ran no selected tests; it is excluded from evidence. The corrected selection
  above explicitly records both intended pass events.
- The next ordinary failfast run advanced past both restored cases and the
  aggregate selection: 52 pass events, eight capability skips and
  one failure, 26.766 seconds. It stopped at TestSQLAlterMultiDbt: indexAttrs
  still constructs an unscoped legacy store. Its RESP3 index inspection and
  immutable ALTER-layout assertions need reconciliation before a complete rerun.
  This is not full-suite success.
- Go formatting, Python compilation and git diff --check pass.

No production execution or storage code changed in this checkpoint. The preceding
[aggregate checkpoint](ordinary-aggregate-validation.md) supplies the last full
snapshot/native validation; it is not a fresh run for these test changes. Full
ordinary-suite success, protocol-15 Cluster/platform/recovery coverage, complete
ACL recipes, process resume and sustained performance parity remain open.
Self-review is not maintainer approval.
