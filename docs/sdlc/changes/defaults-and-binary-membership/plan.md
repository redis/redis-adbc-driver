# Plan

Date: 2026-11-20. Requestor: Albert Wong. Author: Codex. Base: `e11653eb`.

1. Read issues and current parser/default/partition contracts. Isolate the work
   from unrelated checkout changes. Done.
2. Reproduce DEFAULT syntax acceptance and retained numeric input failure with
   focused failing tests before implementation. Done.
3. Restrict column DEFAULT parsing while retaining nested/full SET DEFAULT
   expressions; preserve original literal input types in ALTER's stored SQL. Done.
4. Add unit, SQL lifecycle, prepared-before-ALTER, Arrow omission and independent
   pinned PostgreSQL 18.6 comparisons. Done; final checks recorded in validation.
5. Add a standalone native membership matrix and partition agreement/source
   preservation controls; run on verified Software RAM and actual Flex. Done.
6. Review correctness/lifecycle, ownership/security, and intent/evidence;
   document limitations, complete checks and open a PR linking all three issues.

Files: `go/sql.go`, `go/alter_type.go`, focused DEFAULT tests,
`go/pgdiff/default_repairs.py`, binary-key native script,
`go/unmanaged_partitions_integration_test.go`, user documentation and these records.

See [intent](intent.md), [spec](spec.md), and [validation](validation.md).
