# Plan and evidence

Date: 2026-11-16 (America/Los_Angeles). Source: `225e0056`.
Author: Codex. Requestor: Albert Wong. Status: implemented and locally verified;
human review pending.
Issue: [#449](https://github.com/redis/redis-adbc-driver/issues/449).
See [intent](intent.md) and [spec](spec.md).

1. Add a focused reproduction in `go/sql_index_quoted_integration_test.go`.
2. Persist safe aliases in `go/store.go`; allocate them in `go/sql_index.go`
   only when named-index creation needs an unsafe physical HASH field.
3. Verify native numeric/TAG bindings and lifecycle behavior; add unit
   collision/persistence and unsupported-form checks in `go/sql_index_quoted_test.go`.
4. Document the supported DDL and retained query fallback; format, run appropriate
   checks and review correctness, ownership and evidence under `REVIEW.md`.

## Evidence

- Before the fix, the new integration reproduction failed at
  `CREATE INDEX ... ("Customer ID")` with SQLSTATE `0A000` as reported.
- `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`:
  passed all packages (root package 40.117s). Integration skips are expected
  without endpoints and do not establish server compatibility.
- `make -C go build`: passed, retaining the Makefile version linker flag.
- `make -C go test-software TEST_ARGS='-run "Test(SQLQuotedNamedIndexes|QuotedIndex|SQLNamedIndexAndMatView|IndexAttributesRetry|SQLNewFeatureErrorsAndMetadata|SQLNewFeatureSQLStatesAndDependencies|AlterWaitsBeforeMetadataCommit|RangeReadinessSchemaConvergence|TruncateAttributeReadinessRetry)"'`:
  passed all ten selected root tests and their subtests, with no skips.
  This includes native numeric/TAG alias predicates, metadata, refresh, renames,
  cleanup, collision handling and rejected UNIQUE/expression/partial forms.
- Expanded `TestSQLQuotedNamedIndexes` with SQL ORDER BY, DISTINCT and aggregate
  checks, then reran it with `make -C go test-software
  TEST_ARGS='-run ^TestSQLQuotedNamedIndexes$'`: passed both table and
  materialized-view subtests (root package 7.674s).
- Native ADBC reproduction against the rebuilt shared library: passed with
  Python 3.13.16, adbc-driver-manager 1.13.0 and PyArrow 26.0.1. Created all six
  named indexes across an ordinary table and materialized view with spaces,
  mixed case and embedded quotes/punctuation. Fully fetched and asserted Arrow
  rows/NULLs, refreshed after an insert and verified six catalog entries. Used
  a scratch schema removed in `finally`, after rerunning the Software setup
  wrapper's effective QPF and policy checks. This was a local smoke script,
  not a checked-in adapter qualification or the full native validation suite.
- `gofmt`, `git diff --check` and change-record local-link checks: passed.
- Runtime fixture: dedicated, disposable Redis Software / Redis 8.8.0 with three
  active primary shards and authenticated TLS. `make -C go redis-up` and the
  `test-software` wrapper verify active scaling factor 4, six Search workers,
  `sched_policy=mnp`, `conns=32` and `noeviction` through the management API.
- Client: macOS arm64, module toolchain Go 1.27.3; Docker published-port route.
  This is functional validation, with no benchmark or performance claim.
- Operational setup, endpoint details and credentials remain outside git.
- Removed the dedicated Software container/network and its generated endpoint
  environment file after validation.

## Review

Self-review under `REVIEW.md`: no outstanding actionable findings. Confirmed
that alias allocation occurs on private metadata, aliases survive JSON cloning,
native index builds use separate identifier/alias arguments, catalog membership
keeps physical fields and existing expression-safety guards remain in place.
No row-key, ownership, GC, transport retry or source-index policy was changed.
The physical native-predicate test sums every segment of this append-only
fixture; inspecting only the newest segment would miss baseline rows.

Limits: the full Software suite, full native validation suite and downstream
dbt adapter qualification were not run. No benchmark, human approval, merge or
release is recorded. No alias-based query optimization is claimed.
