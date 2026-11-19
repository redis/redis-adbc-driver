# Validation tracking

Date: 2026-11-19. Author: Codex. Status: implementation and integration validation in progress.
See [intent](intent.md), [specification](spec.md), and [plan](plan.md).

Baseline release evidence is historical: v0.0.30's primary ADBC runs reported
568 passed, eight capability skips and three passing subtests. The requested
seven skips are the change's targets; transaction toggling remains excluded.

Parallel implementation uses isolated worktrees. Redis integration operations
must hold a serialized fixture lease and execute through the root checkout's
Software setup wrapper, which verifies actual endpoint ownership, active 4x QPF,
effective Search/routing configuration and noeviction before passing test settings.
The shared endpoint must never run destructive snapshot tests concurrently with
ordinary/native suites. Raw logs, environment/connection data and operational
artifacts remain in ignored build outputs. No ClickBench run is requested.

## Completed checks

- Root unit-only Go suite (`env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test
  -count=1 -p 1 ./...`) passed before catalog/constraint integration.
- #537: the named upstream table-type test and two native filter regressions
  passed (three tests, no skips). Restricted-reader table-type metadata checks
  passed without granting metadata write privileges. Software setup verified
  Redis Software 8.8.0, three shards and active 4x QPF before execution.
- #535/#536: isolated implementation passed its full unit-only Go suite and
  `TestSQL(Declaration|Constraint|Check|PrimaryKey)` on wrapper-verified Software
  8.8.0 with active 4x QPF. Combined catalog/native validation remains pending.

The above isolated results do not substitute for final integrated validation.

## Combined native validation

The Makefile-built source library, Python 3.14.10 and native driver manager passed
**580 tests, one skip and three subtests** on verified Redis Software 8.8.0 with
three shards and active 4x QPF. The sole skip is transaction toggling (#542),
which is outside this change. The XML report confirms all seven requested
upstream cases executed and passed:

| Issue | Upstream case |
|---|---|
| #535 | `test_get_objects_constraints_foreign` |
| #536 | `test_get_objects_constraints_unique` |
| #537 | `test_get_objects_tabletypes_filter` |
| #538 | `test_get_table_schema_catalog` |
| #539 | `test_set_current_catalog` |
| #540 | `TestIngest.test_catalog` |
| #541 | `test_replace_catalog` |

The run also includes same-name/different-data catalog isolation, ordered UNIQUE
and FK metadata, table/column rename and name reuse, same-column-name Arrow
replacement invalidating old reference usage, and unknown-catalog refusal before
input consumption. Final qualified-DDL registry dependency refinements were
subsequently added; native confirmation after that refinement remains pending.

Separately, integrated disposable catalog SQL lifecycle and selected-generation /
reserved-name boundary tests passed on the verified fixture. Full integrated
unit-only Go packages passed. Broader Software Go integration is in progress;
its first failure exposed an existing cleanup recovery test's assumption that
its next global collector step must select a specific garbage generation.
Investigation and a bounded-progress test correction are in progress; no skip or
production cleanup relaxation is proposed.
