# Main integration and SQL feature parity

Date: 2026-11-03. Merge parents 6a35be9 and main 6d7ac40.
Status: incomplete parity; [plan](plan.md), [specification](spec.md).

## Scope and implementation

Main added generated keys/sequences, UUIDs, modifying CTEs, named indexes and
materialized views after this snapshot branch diverged. The merge had conflicts
in README, ingestion, writable-table checks and table metadata. Both feature sets
are retained. The example/SQL guide now describes current snapshot behavior.

Snapshot statement classification admits the new DDL and treats a SELECT with
modifying CTE bodies as a write. CTEs retain frozen statement values while their
target identities are rebased onto private deltas. A late outer error cannot
publish earlier CTE writes. Named index field additions build a private layout
and defer its one final index; they never alter a published source index.
Dropped-column index names and owned sequence dependencies retire in the same
private catalog. Materialized refresh swaps registered private descriptors and
retains old generations through normal references; it never drops old shared
HASHes/indexes directly.

Sequence definitions participate in immutable metadata. Reference-owned native
counter values are explicitly volatile: exact int64 allocation uses opaque JSON
CAS, reader/writer admission and current-definition proofs. Only proven CAS
refusals retry already evaluated allocation arguments; an uncertain reply fails
without resend. Connection caches include definition/reset/SETVAL invalidation.
Restart definitions are published atomically; NEXTVAL/SETVAL allocations remain
nontransactional and can leave gaps after failed SQL. Counters stay alive for old
pins and are reclaimed after their last metadata reference.

The storage boundary is now **protocol 15**, with rejection of protocol 14 and
earlier formats and no migration. Older snapshot binaries do not understand
materialized-view write protection or volatile counter behavior; retaining their
format number would permit unsafe mixed-version writes. Historical protocol-14
Cluster, recovery and benchmark evidence remains historical, not protocol-15
validation.

## Reproductions and focused checks

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries behind the proxy,
Search, noeviction and active effective 4x QPF verified using make -C go redis-up.
Database-mutating suites run sequentially on macOS ARM64. Operational artifacts,
credentials and infrastructure identifiers remain private.

Before adaptations, upstream tests rejected sequence/materialized/index DDL;
modifying CTEs attempted to write published/shared data. Subsequent focused runs
caught committed definition owners being mistaken for private writers and stale
dropped-column index registrations. These failures were retained in private logs
and corrected without weakening assertions.

- All eight upstream SQL feature integration tests passed with race detection,
  6.652 seconds. Six independent connections allocate 120 unique cached values;
  bounds, exact int64 maximum, SERIAL/identity overrides/restarts, ownership,
  UUIDs, materialized refresh and index dependency checks pass.
- Three added snapshot integration cases passed with race detection, 2.400
  seconds: late CTE rollback/statement preimage, sequence gaps with no partial
  rows or definition/root mutation, old-pin counter retention and post-release
  reclamation, and one-attempt lost allocation reply handling.
- Unit-only packages: 919 pass events, 808 expected integration skips, zero
  failures. These focused/unit runs preceded the protocol-15 boundary change.
- Final protocol-15 unit-only packages: 919 pass events, 810 integration skips,
  zero failures.

The first protocol-15 full snapshot run caught an additional retention regression:
new collision WATCH probes copied absent view/sequence/index entries into the
private catalog. The existing deleted-name reclamation test failed. WATCH now
records immutable read dependencies and leaves copy-on-write to actual mutations,
avoiding both absent mappings and unnecessary native copies. The focused
deleted-name case passes after this fix. A new cross-session SETVAL/cache and
stale-definition case also checks serialization refusal before allocation.
All four new SQL snapshot cases and deleted-name reclamation pass in the final
protocol-15 run; the stale-definition test preserves the driver's existing
ADBC I/O mapping for SQLSTATE 40001.

Final protocol-15 validation after the WATCH correction:

- Complete snapshot race suite: **224 pass events, 14 explicit skips, zero
  failures**, 298.541 seconds. Nine separate Cluster cases and five optional
  permission-principal cases are skipped; no protocol-15 Cluster/recovery claim.
- Rebuilt versioned native C/ADBC snapshot suite: 13 passed, 12.720 seconds.
  Main's generated-key/UUID/modifying-CTE/index/materialized-view Arrow/C ABI
  test passed under Python 3.14.10, 0.40 seconds. IPC: nine passed, 1.08 seconds.
  TLS Ray: 17 passed, 13.03 seconds, three upstream deprecation warnings and no
  interface skips.
- All eight upstream SQL feature race tests pass again on protocol 15, with
  no skips or assertion changes, 6.328 seconds.
- Go formatting, documentation links, failure inventory JSON and git diff
  --check pass. Protocols 6 through 14 are explicitly rejected without modifying
  their catalogs. Active-Active remains excluded; noeviction remains required.

## Complete ordinary suite: failing evidence

The complete ordinary Go package run completed, with **966 pass events, 556
failure events and 226 skips**. Root package elapsed 247.153 seconds; CLI package
also failed. [Full failed-test inventory](main-merge-failure-inventory.json)
preserves names and package outcomes without private operational logs.

These events include nested failures and cascades, not 556 independent defects.
Early ACL and aggregation helpers construct legacy stores and cannot find
snapshot table metadata. Later cases hit the 16-generation admission limit;
retained cases also report missing verified control-persistence declarations.
The same feature tests that passed in isolation later fail with admission-limit
errors in the full run. Every failure still needs isolation/triage and a complete rerun;
this evidence does not dismiss product regressions as test-only problems. No
skip or assertion was broadened to make the run green.

## Review and remaining work

Reviewed preservation of both merge sides, private CTE target identity, frozen
values, final index lifecycle, materialized-view reference ownership, exact
counter arithmetic, CAS/no-resend behavior, reset/cache invalidation and counter
reclamation. Self-review is not maintainer approval.

Full ordinary feature parity is **not achieved**. Protocol-15 Cluster/native
platform/failover/persistence checks, wider sequence concurrency/failure/performance
coverage, process resume, sustained compaction measurements and broader
performance comparisons remain open. No performance improvement is claimed for
the new sequence allocator's additional metadata/counter reads.
