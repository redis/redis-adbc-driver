# Original application keys in managed snapshots

Date: 2026-11-03. Parent source `de00669`. This checkpoint restores imported
key identity; [full feature and performance parity](plan.md) remains incomplete.
Storage protocol **14** rejects legacy storage and draft formats 1–13 without
modification. No migration or disable switch is provided.

## Behavior and implementation

Adopted HASH and foreign-index managed copies retain each original application
key as the `__key` system column. It is distinct from the physical managed row ID,
excluded from `SELECT *`, public table schemas and information_schema.columns.
Import writes provenance in the same HSET as the row rather than issuing a
second write. SQL-added rows have NULL provenance. UPDATE/DELETE RETURNING,
compaction, ALTER/rekey, old streams and shared-generation partitions preserve
captured identity. Explicit refresh recaptures source rows and inferred schema,
replacing private SQL changes; SQL never modifies application HASHes or indexes.

An internal TAG attribute supports candidate selection, with conservative exact
HASH rechecks for unsafe strings. Numeric comparisons against `__key` cannot
become physical row-ID lookups. Generic HASH decoding, narrow field loading,
positional Arrow projection and late projection load the hidden field separately
from row identity. Generic decoding does not copy the metadata slice per row.

## Regression evidence

The earlier report checkpoint exposed missing `__key` (42703) in TestAdoptHashes.
That test now verifies the agreed private-copy ownership model while retaining
its original type/value, nested-key and exact-key coverage. Added assertions
require independent internal IDs, unchanged source HASHes, atomic typed-refresh
failure, explicit refresh visibility, private NULL provenance and normal DDL.
Other adoption regression families still need reconciliation; this is not a
claim that the whole adoption suite passes.

A focused ordered residual/LIMIT case reproduced NULL instead of the original
key in late projection. The fix includes provenance in the late field list.
The regression forces the unrestricted-cap planner branch on a single row that
fits the real server cap and asserts that late projection was actually chosen.
The separate native case validates residual/sort/LIMIT results under real caps;
it does not claim that its planner necessarily chose late projection.

Additional tests cover NUL-distinct keys, Unicode/TAG separators, exact integers
above 2^53, duplicate projected keys, independent row IDs, tombstones, NULL
provenance, full old-stream values and complete partition coverage after
refresh/TRUNCATE. The pinned partition readers retain original key/value
pairs after writes, compaction, ALTER, rename/rekey and refresh.

## Final validation

Owned Redis Software fixture on macOS ARM64: three primary shards, Search,
strict TLS, active effective 4x QPF and noeviction verified by `make -C go redis-up`.
Database-mutating suites ran sequentially.

- Full `TestSnapshot` race run: 138 pass events, seven expected Cluster-only
  skips, zero failures, 133.851 seconds.
- Unit-only `go test -json -count=1 -p 1 ./...`: 900 pass events, 715 expected
  integration skips, zero failures. These skips are not compatibility evidence.
- Final native library built with `make -C go build` and its version linker flag.
  Native C/ADBC snapshot validation: 13 passed in 14.231 seconds.
- Ordinary TestAdoptHashes race run: passed, package time 3.949 seconds.
- Rebuilt-library Software API schema-isolation gate: metadata/public HASH/Search
  positive controls and six cross-schema negative checks passed. Owned resources
  and principals were removed.

Corrected full interface run: native IPC nine passed (1.32 seconds); Ray 17
passed (13.29 seconds, three configuration deprecation warnings), zero interface
skips. CLI TestAdoptCommand race run passed (2.195 seconds package time).
Changed Go formatting, local Markdown links and `git diff --check` pass.

An initial
IPC/Ray invocation mistakenly cleared the REDIS_URI expected by pytest fixtures:
IPC skipped nine and Ray passed ten/skipped seven. That run provides no native
interface compatibility evidence; the corrected invocation retains REDIS_URI.

## Scope and author review

Reviewed provenance versus physical identity, public schema hiding, exact HASH
rechecks, private-row NULLs, ALTER conversion, same-command row writes, old pin
retention, partition release and source ownership. Self-review is not maintainer
approval. This change adds an internal index attribute and stored field; no
throughput or performance-parity claim is made without comparative measurement.

Remaining work includes other adoption/sweep/ACL regressions, restricted-driver
command/pin recipes, resumable large operations, sustained compaction performance,
current Cluster API/failover/persistence recovery, the full platform matrix and
broader comparative benchmarks. Historical results retain their actual formats.

## Follow-up compatibility inventory and regression reconciliation

The ordinary Software race inventory selected `^(TestAdopt|TestAppIndex|TestSweep)`
at `ad46699`: three pass events and 14 failure events (including text-encoding
subtests and their parent). It is a deliberately red inventory, not full-suite
validation. Failures covered legacy raw metadata lookup, an obsolete schema-prefix
blocker, blocked-Apply error expectations, old index-creation callbacks and
adopting registries, explicit application-key INSERT, old index-reuse reports,
legacy sweep helpers and text-encoding tests expecting live source updates.
The regex does not include every ACL or foreign-discovery case.

Follow-up tests reconcile text encoding, SQL foreign-index discovery and schema
prefix overlap with the managed-copy contract. They retain typed Boolean/date/
time/timestamp queries, original external text formats, source-index survival,
filtered foreign membership, read-only foreign schema enforcement and discovery
metadata checks. Added assertions verify canonical private UPDATE results,
external changes invisible before refresh, source-value restoration on refresh,
original keys after refresh, and source HASH preservation after same-named schema
DROP. No production code changed in this follow-up; low-level unmanaged HASH
race coverage remains intact.

Focused current race run selected
`^(TestAdoptTextEncoding|TestSQLForeignIndexDiscovery|TestAdoptedPrefixOverlap)$`:
all three top-level cases and both encoding subcases passed, zero skips/failures,
3.506 seconds package time. This does not establish that other inventory failures
are repaired. Remaining families require their own behavior and recovery checks;
no assertions were skipped or broadened to hide a failure.
