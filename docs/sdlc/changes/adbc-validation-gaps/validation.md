# Validation tracking

Date: 2026-11-19. Author: Codex. Status: implementation complete; local validation recorded below.
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
  8.8.0 with active 4x QPF. Combined catalog/native validation subsequently passed below.

The above isolated results do not substitute for final integrated validation.

## Combined native validation

The initial combined run with the Makefile-built source library, Python 3.14.10
and native driver manager passed
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
input consumption. Final qualified-DDL registry dependencies and advanced-execution scope guards
are included in the final native confirmation recorded below.

## Final integrated checks

- All Go packages passed the final unit-only suite (`env -u REDIS_URI -u
  REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`).
- `^TestSnapshotCatalog` passed with the disposable snapshot URI: seven top-level
  tests, including the three new lifecycle, stale-selection/boundary, and
  in-flight qualified-DDL DROP/recreate conflict tests. These names participate
  in the existing dedicated snapshot CI shards.
- Twenty-five focused ordinary Go tests passed: catalog identity/command-policy/
  advanced-refusal tests, declaration lifecycle and snapshot ALTER/same-name Arrow
  replacement tests, CHECK/PK regressions, schema-scope errors, COMMENT errors and
  temporary-table compatibility.
- Cleanup recovery passed ten repeated paired runs. The original full ordinary
  run exposed a queue-order assumption; the isolated original pair passed ten
  runs, confirming the failure depended on broader queue state rather than
  reliably reproducing in isolation. The corrected helper permits bounded queue
  progress under the original ten-second deadline (less than the two-minute
  snapshot lease), checks pinned data on every step, requires exactly three
  target deletions, and retains index/directory/graph removal assertions.
  No production cleanup code or skip was changed.
- Actual Software RBAC tests passed for both default metadata/table-type filters
  and named-catalog GetObjects/GetTableSchema. Discovery made no management ACL
  writes. Registry access uses existing HGET/HGETALL commands and a narrow
  read-only `workspace:*:catalogs` metadata key grant. Policy regressions retain
  exclusion of captured import values, journals, private workspace payloads and
  managed row keys. Existing roles need the current generated reader policy.

The first full ordinary Software Go run completed all packages and exposed five
failures: the cleanup assumption, three old cross-catalog error expectations,
and temporary-table error wording. All five were corrected and their focused
reruns passed. Other tests in that full run passed or retained their existing
optional/environment skips; protocol ACL-user tests remain unavailable through
Software's Redis endpoint, while the real management-API RBAC tests above ran.

## Final native confirmation

The final source library passed **581 tests, one transaction-toggle skip and three
subtests** on the same wrapper-verified Software configuration. All seven named
upstream cases passed in the final XML report. The additional test verifies
named-catalog GetObjects/GetTableSchema stay pinned through ALTER and table
replacement, show fresh metadata after release/acquire, and continue refusing
unsupported named-catalog row execution.

An intermediate final run exposed two existing shared-snapshot regressions from
an overbroad named-catalog metadata refusal. The fix retains whole-root pinned
metadata discovery while continuing to refuse unverified named row execution.
Both existing shared-snapshot tests and the new metadata test passed together,
then the complete 581-test suite passed. No test was skipped or weakened to
resolve this regression. The actual restricted-reader key refusal was likewise
reproduced, fixed by the exact registry metadata read pattern, and rerun
successfully with unchanged management ACLs during discovery.

A subsequent hosted TLS checker still expected PK/CHECK-only metadata. Its
exact assertion now includes named composite UNIQUE/FK declarations and ordered
logical reference usage through rename, drop and re-add. The exact corrected
metadata acceptance function passed all six independent permission cases on
verified 4x QPF Software with verified data TLS. Existing statistics and denial
checks remain intact, and its temporary API principals/schema were removed.
This checker-only follow-up does not change the validated native implementation.

Hosted final-source, full-suite checks are tracked on
[PR #543](https://github.com/redis/redis-adbc-driver/pull/543/checks). Raw logs and
XML reports remain in ignored build outputs; no operational identifiers,
credentials, endpoint configuration or license artifacts are included here.
