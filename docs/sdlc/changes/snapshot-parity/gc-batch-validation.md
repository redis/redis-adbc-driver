# Durable row reclamation batches

Date: 2026-11-03. Parent source 2f43d1b. Protocol remains 14, without migration.
[Full parity](plan.md) remains incomplete.

## Reproduction and implementation

An actual private, indexed 600-row generation was aborted and its metadata
references retired. A hook injected an interruption after the first successful
256-row deletion reply. Before the fix, its directory still contained all 600
IDs. A fresh collector would rescan already deleted rows. Repeated short budgets
could therefore prevent a large generation from finishing.

Row cleanup now uses one non-retried Lua command per page, at most 256 rows.
It verifies the registered canonical prefix, deleting owner, zero references and
queue membership. The complete batch's exact positive int64 IDs, row-key mappings
and zero directory scores are checked before mutation. IDs never pass through
Lua floating point. DEL and ZREM permissions are preflighted before either call,
because a Lua runtime error would not roll back an earlier mutation.

The command deletes only still-registered rows and removes the corresponding
directory members atomically. The remaining directory is durable progress;
there is no persisted SCAN cursor or in-memory recovery requirement. A stale
collector skips consumed entries, including after another collector has removed
the completed ownership record. Unregistered objects at old keys remain intact.
Confirmed counts include only acknowledged batch replies. An interrupted caller
can undercount deletions whose reply was lost; it must not report them confirmed.

This changes no live manifest, record format or ownership rule. Only deleting,
zero-reference data loses directory entries. Older protocol-14 collectors can
consume the remaining entries and finish safely, so no protocol bump is needed.
Reader pins, writer/compaction references and immutable live directories retain
their existing protections. Background budgets and explicit sweep round limits
remain in effect. Index drop and final ownership retirement retain their prior
protocol; no SQL, Arrow input or uncertain schema operation is replayed.

## Validation

The owned Redis Software fixture was reverified using make -C go redis-up:
strict TLS, noeviction and active effective 4x QPF. Database suites run sequentially
on macOS ARM64. Logs and fixture identifiers remain private.

- Before-fix lost-reply regression failed: directory remaining 600, expected 344.
- After-fix lost-reply regression passed: a fresh client reclaimed exactly 344
  remaining rows in two batches, starting at row 257.
- Focused GC/sweep race checks passed before adding repeated-interruption and
  restricted-principal coverage. Final validation is recorded below.
- Complete snapshot race suite: 164 pass events, seven expected Cluster-only
  skips, zero failures, 135.393 seconds. This includes repeated interruption on
  every row batch, int64 maximum IDs, rejected unclaimed/referenced/malformed
  batches and stale-collector protection after ownership retirement.
- Unit-only packages: 906 pass events, 747 expected integration skips, zero
  failures. These counts precede the separately run optional ACL test addition.
- The Software API helper scripts/snapshot_gc_acl.py created a temporary data
  principal with DEL allowed and ZREM denied. Its dedicated Go race test passed
  (1.828 seconds), verifying both command controls and the script's own permission
  error before any row/directory mutation. Principal cleanup completed.
- Shared library rebuilt with make -C go build, preserving its version flag.
- Rebuilt native C/ADBC snapshot validation: 13 passed, zero skips/failures,
  14.142 seconds.
- Changed Go formatting, Python helper parsing, evidence links and
  git diff --check passed. IPC/Ray were not rerun for this internal GC change;
  their prior checkpoint results are historical.

## Review and limits

Reviewed admission before mutation, exact int64 handling, directory ownership,
stale/concurrent collectors, partial reporting and no command replay. This is
self-review, not maintainer approval. The permission probe isolates GC preflight;
it does not prove the complete least-privilege driver command/pin recipe.

Large SQL/Arrow operation resumption, sustained compaction throughput, full
platform/latest Cluster/failover/persistence checks and comparative performance
remain open. No throughput or full performance-parity claim follows from these
bounded reclamation tests.
