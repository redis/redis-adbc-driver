# ALTER actions and constraint snapshot regression reconciliation

Date: 2026-11-03. Parent 8f84c3a. Storage remains protocol 15; no migration.
[Full feature and performance parity](plan.md) remains incomplete.

## Reproduction and current lifecycle assertions

The ordinary suite stopped at TestSQLAlterMultiDbt because indexAttrs constructed
an unscoped legacy store. Index inspection now acquires and retains an immutable
statement generation through FT.INFO, and normalizes RESP3 attributes with the
existing parser. tableColumn also reads pinned metadata. No driver admission
checks are disabled.

The first restored run passed the atomic-error and syntax/view-error families,
but exposed three incompatible fixture assumptions: indexes contained only the
old system fields; dropped columns accumulated indefinitely in the same index;
and every concurrent same-table ALTER necessarily committed. Snapshot layouts
include the deletion marker, rewrite into fresh private indexes containing only
current fields, and return documented SQLSTATE 40001 to losing writers. Exact
index assertions now include the deletion marker and the current fields. Retired
physical field names remain reserved in metadata and those assertions remain.

The dbt add/remove sequence, action ordering, defaults, CHECKs, NULL/NOT NULL,
string lengths, rename behavior, GetObjects/schema metadata, physical dropped
field removal, and unchanged metadata/index/rows after every rejected action are
still exercised. No case is skipped or converted into an expected failure.

TestSQLAlterLeftoverAttribute now simulates a late error after Redis actually
creates the final private index, rather than mutating a published index using
FT.ALTER. It requires one create attempt, the actual index's private prefix and
schema, unchanged published metadata/rows/index, successful subsequent ADD COLUMN
operations with uncontaminated field names, correct filtering/ordering, unchanged
pinned source rows/index, and eventual orphan-index/row reclamation. The first
fault-fixture run incorrectly compared the planned index name with the final
sealing identity; the corrected fixture validates the actual created index's
registered data identity and prefix. That failed fixture run is not pass evidence.

The concurrent test still submits all 18 multi-action changes across three
connections. Every attempt is classified: a successful ALTER must expose both
columns, their exact values for both rows, and its working CHECK; a rejected
ALTER must report 40001 and expose neither column nor constraint. Complete
metadata/index cardinality must match only committed changes. A pinned original
source retains its original physical rows. Statements are never replayed to
force every attempt to succeed. This follows the published conflict contract;
it does not add transparent concurrency retries.

## NOT NULL and concurrent constraint validation

The subsequent ordinary run advanced to 59 pass events and eight capability
skips before TestSQLAlterNotNullMissing failed its virtual-default field-count
assumption (34.709 seconds). Snapshot ALTER materializes the default in a
replacement: the test now requires both physical values and no stale virtual
metadata, while retaining NULL-update, DROP DEFAULT, later-insert and rejection
checks. This validates the current materializing implementation; it does not
establish metadata-only DDL cost or performance parity.

TestSQLAlterNotNullConcurrent no longer relies on the legacy mutable-ALTER hook
or writes directly using stale descriptors. It deterministically stages private
SQL before publishing an intervening public statement. All seven stale schema or
write publications must return 40001. Fresh SQL then validates the actual current
rows/schema: NULL and negative CHECK values reject constraints, valid intervening
rows allow a new constraint statement, a concurrent DROP NOT NULL survives the
losing redundant SET, and an old nullable write cannot commit after NOT NULL.
An added NOT NULL default appears on explicit fresh inserts; adding NOT NULL
without a default to an empty table fences a previously staged insert. Rejected
writes leave no visible partial rows. No uncertain statement is replayed.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified through make -C go test-software. Mutating
suites run sequentially on macOS ARM64. No ClickBench run or reload.

- Final six-case multi-action ALTER race selection: all six passed with zero
  skips/failures. Per-case times: dbt 0.44s, mixed actions 0.99s, atomic rejection
  1.22s, syntax/view errors 0.32s, private-index failure/recovery 0.37s, concurrent
  changes 1.09s. The concurrent run recorded eight complete commits and ten
  serialization conflicts, with no implicit replay.
- Final expanded ALTER/NOT NULL race selection: all nine top-level cases passed,
  zero skips/failures. The concurrent multi-action sample recorded seven commits
  and eleven conflicts. All seven deterministic constraint/write conflicts and
  subsequent fresh-statement validation passed; final NOT NULL cases took 1.31,
  0.61 and 0.94 seconds; the root package completed in 9.636 seconds. Earlier
  six-case evidence above remains labeled.
- Unit-only packages: 919 pass events, 812 expected integration skips, zero
  failures. Integration skips do not establish platform or server compatibility.
- The final ordinary failfast run advances to 63 pass events and eight
  capability skips, then stops at TestSQLAlterTypeValues: its direct metadata
  read at alter_type_integration_test.go:234 still lacks a statement scope.
  Root package duration: 33.528 seconds. Remaining type/rewrite fixtures and
  their immutable-layout/recovery assertions need reconciliation; this is not
  a successful complete-suite run.
- Go formatting and git diff --check pass. Local evidence links resolve.

No production execution or storage code changes in this checkpoint. The preceding
[aggregate checkpoint](ordinary-aggregate-validation.md) supplies the last full
snapshot/native validation, not a fresh full run for these fixture changes.
Complete ordinary-suite success, protocol-15 Cluster/platform/recovery, ACL
recipes, process resume, sustained compaction and comparative performance remain
open. Self-review is not maintainer approval.
