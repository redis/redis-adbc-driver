# Constraints, creation zones and default-column fixtures

Date: 2026-11-03. Parent fc22225. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproductions and corrections

The previous ordinary diagnostic stops at TestSQLConstraintForms after 429
passes and ten capability skips. Its rejected-CREATE absence check uses an
unscoped legacy tableExists. Both constraint-definition absence assertions now
require NOT_FOUND from public GetTableSchema. Removed CHECK metadata is inspected
under a pin. Existing precise syntax/type/constraint errors, unchanged rows and
definition checks remain.

The first focused selection records ten passes and one failure in 21.105 seconds:
TestSQLCheckLifetime expects a table-name prefix after forced rekey. It now
requires fresh canonical UUID storage/indexes, all three exact rows and unchanged
pinned source images. Normal reference-aware GC must reclaim the old output only
after pin release. Constraints and all metadata interfaces retain their checks.

All three creation-zone tests separately fail at their raw metadata-edit helper
(root package 3.022 seconds). It now reads a pinned definition, stages its date
literal edit through a registered private metadata value, publishes normally and
checks the original stored bytes under the old pin. Views, defaults and CHECKs
retain New York/UTC/Kolkata, DST, literal/conversion and pushed-predicate checks.
These are test definitions in protocol 15, not a storage upgrade path.

The defaults selection initially records six passes and two failures (17.243
seconds). ADD COLUMN materializes existing values, while the legacy fixture
expects absent fields, virtual MissingThrough and NULL markers. The corrected
fixture requires exact materialized field counts, default metadata, native index
predicates and absent fields for explicit NULLs. It retains every row/value,
sort/aggregate/join/subquery/view, DML, rename/drop and identity assertion.
Virtual-value predicate expansion and residual guards remain checked separately
on a copied planner descriptor; published metadata and rows are untouched.

The stale-INSERT fixture stages an actual private INSERT before two ADD COLUMN
statements. Publication must return 40001 with the current root unchanged, no
leaked row, exact old pinned input and confirmed private-output reclamation. A
fresh explicit client INSERT then reads the new default; a later explicit NULL
stays NULL. No implicit SQL/input replay, mutable source write or compound
transaction is introduced. Field-materializing ALTER still rewrites data; this
checkpoint does not claim improved DDL throughput.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified through make -C go test-software. Mutating
suites are sequential on macOS ARM64. No ClickBench run/reload. Operational logs,
credentials and infrastructure identifiers remain private.

- Corrected constraint/creation-zone selection with `-race -count=1 -p 1`:
  14 top-level passes, zero skips/failures, root package 21.302 seconds. The
  selection also includes the neighboring checked-parallel-input case.
- Corrected defaults selection with the same race/cache/package settings:
  eight top-level passes, zero skips/failures; root package 19.321 seconds.
- Final unit packages: 923 pass events, 822 expected integration skips, zero
  failures. Integration skips do not establish server compatibility. Go
  formatting, local evidence links and diff whitespace checks pass.

The subsequent ordinary diagnostic with `-json -failfast -count=1 -p 1 -skip
TestACLAppIndexReader` reaches 462 pass events, ten capability skips and one
failure; root package 171.953 seconds. It passes the restored constraint and
creation-zone cases, then stops at TestFractionalPrecisionIssue113's unscoped
metadata helper. Later ordinary cases, including the defaults family already
checked independently above, did not execute in that failfast run. The known ACL
test remains excluded only on this diagnostic command line. No repository skip
was added, and complete ordinary success is not claimed.

## Author review and remaining work

Reviewed specific error/status assertions, canonical physical IDs, pinned rows
and stored metadata, ownership-aware cleanup, private planner copies, publication
conflicts and explicit client continuation. This is not independent maintainer
approval. No production code or storage format changed. Full ordinary/current
Cluster/platform/recovery, restricted reader ACL integration, process resume,
sustained compaction and broader performance gates remain open.
