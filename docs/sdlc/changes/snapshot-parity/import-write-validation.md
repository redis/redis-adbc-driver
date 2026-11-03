# Managed import writes, replacement and provenance joins

Date: 2026-11-03. Parent source a842a23. Storage remains protocol 14; no migration.
[Full feature and performance parity](plan.md) remains incomplete.

## Reproduced gaps and corrections

Arrow replace dropped a managed import's origin. Its new ordinary descriptor
lost the __key system column (42703), and subsequent explicit refresh could no
longer recognize its destination. The new regression reproduced the missing
column before the fix. Replacement now reads the current descriptor under the
statement pin and preserves SnapshotImportSource/SnapshotSourceKey when staging
the new table. Its public schema and data still come entirely from Arrow; new
rows have NULL provenance. Old generations remain independently pinned. A later
explicit refresh recaptures source schema/rows and replaces private changes.

A MERGE matching on target __key also reproduced 42703: joined relations hid every
system column. Managed provenance is now available as a qualified logical column.
Joined scan views bind the hidden HASH field to its qualified name, separately
from the physical managed row ID. Joined type/column lookup includes provenance
without adding it to public star/NATURAL columns. Output labels remain __key,
matching ordinary column labels. Explicit projections through derived tables and
CTEs initially failed too; a real output column of an in-memory relation is now
bound as its projected column rather than hidden as a storage intrinsic.
Direct joined references to stored physical __rowid remain outside this change.

## Regression reconciliation and ownership

TestAdoptKeyedWrites now verifies the agreed managed-copy write contract:
INSERT/SELECT/RETURNING, prepared types, MERGE insert/update, Arrow append and
create_append, duplicate public values with distinct managed IDs, mixed TRUNCATE,
restart and explicit refresh. Application __key provenance is read-only; explicit
SQL/MERGE/Arrow attempts to write it fail without partial logical results. New
private rows have NULL provenance. No truncate opt-in is required for managed
copies; the legacy option's strict Boolean validation remains checked. Every
original application HASH, nested HASH and non-HASH key remains unchanged.

The low-level unmanaged keyed collision test remains active on a separate owned
raw fixture, with its exact prefix reservation and no fabricated legacy table
metadata. Both writers pass preflight; exactly one atomic writer succeeds and
the other reports the collision. This preserves the underlying race coverage
without claiming SQL mutates live external HASHes.

Replacement coverage requires a late Arrow input failure after consuming its
batch to leave the root, previous schema, rows and origin unchanged. A successful
replace keeps the original stream's full key/value coverage and permits explicit
refresh afterward. Checked input allocation must return to zero.

Join coverage includes NUL-distinct/Unicode keys, inner/full/left joins, NULL
provenance that cannot match another NULL, ambiguous unqualified keys, public
star/NATURAL schemas, derived/CTE projections, correct output names, UPDATE FROM
and DELETE USING with physical target identity, and unchanged application data.
An initial outer-join expected-order typo was corrected to lexical order; actual
ordering was correct. Missing output labels were repaired in virtual metadata.

## Verification

Owned Redis Software fixture: Search, strict TLS, active effective 4x QPF and
noeviction reverified with make -C go redis-up. Database-mutating suites ran
sequentially. Local machine is macOS ARM64.

- Before correction: Arrow replacement lost __key (42703); MERGE provenance match
  and derived-table provenance match independently reproduced 42703.
- Initial corrected write/replace/collision run: three cases passed, 3.326 seconds.
- Focused join/write run after output-label and expected-order correction: four
  cases passed, no skips/failures, 3.942 seconds.
- Final expanded ordinary race run covers the new cases and existing SQL join,
  MERGE/dbt, UPDATE FROM/DELETE USING, RETURNING, NATURAL/LATERAL and scope tests:
  20 top-level cases passed, zero skips/failures, 22.778 seconds.
- Final unit-only `go test -json -count=1 -p 1 ./...`: 904 pass events,
  730 expected integration skips, zero failures. Skips do not prove compatibility.
- Full current snapshot race suite: 144 pass events, seven expected Cluster-only
  skips, zero failures, 132.218 seconds.
- Final native library rebuilt using make -C go build with version metadata.
- Final native C/ADBC snapshot validation: 13 passed, no skips/failures,
  14.042 seconds. New imported-write regressions above use the Go ADBC API;
  this native run validates the existing C interface cases on the same source.
- IPC: nine passed, zero skips, 1.11 seconds. Ray: 17 passed, zero skips,
  16.61 seconds, three configuration deprecation warnings.
- CLI TestAdoptCommand race case: passed, zero skips/failures, 2.175 seconds.
- Complete ordinary `^TestAdopt` race family: ten top-level cases, 14 pass
  events including subcases, zero skips/failures, 8.139 seconds. ACL tests use
  separate TestACL names and are not included; sweep is also outside this run.
- The final replacement regression additionally requires exactly one consumed
  Arrow batch before its injected late error and zero checked input allocation.
  Its focused race run passed in 2.250 seconds. This strengthens the test only;
  production source is unchanged from the full snapshot/native runs above.
- Changed Go formatting, local Markdown links and git diff --check passed.

## Review and remaining scope

Reviewed replacement descriptor dependencies, origin versus public schema,
private row IDs versus provenance, alias/type/field mapping, outer NULL behavior,
star/NATURAL visibility, source ownership, late-input rollback and input ownership.
Self-review is not maintainer approval. No SQL/Arrow input or uncertain operation
is replayed, no external mutation capability is added, and no performance parity
claim is made. Full sweep/ACL compatibility, resumable large operations, sustained
compaction, latest Cluster recovery/platform matrix and comparative benchmarks
remain open in the active goal.
