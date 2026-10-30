# Validation: adopted writes

Date: 2026-10-30. Source: `95d6583` plus working-tree changes. Author: Codex.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

## Original worktree validation (before rebase)

Host: macOS arm64, Go 1.27.2 (module minimum 1.26.6). Disposable Docker Compose project
`redis-adbc-64`, Redis Software 8.6.2, one shard, Search enabled, database port
28379. No existing test databases used. This is not cluster or multi-shard
validation. Integration tests modify only the disposable database.

### Checks

- Unit-only: `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`
  passed. Redis-dependent integration tests skip without REDIS_URI.
- Initial focused Redis Software tests for TestAdoptKeyedWrites,
  TestAdoptInsertCollisionAtomic and TestAdoptHashes passed.
- Full Redis Software suite: `cd go && REDIS_URI=redis://localhost:28379/0
  go test -count=1 -p 1 ./...` passed all three packages (driver 108.096s).
  The compact output does not enumerate capability-dependent integration skips.
- Native library: `make -C go build` passed, producing the host arm64 dylib.
- Final unit-only pass: all three packages passed (driver 9.799s).
- Expanded focused race tests: `cd go && REDIS_URI=redis://localhost:28379/0
  go test -race -count=1 -v -p 1 -run
  'TestAdopt(KeyedWrites|InsertCollisionAtomic)$' .` passed both tests with
  no skips or race reports (1.780s). Covers create_append and rejected replace,
  and mixed managed/adopted truncate refusal in addition to initial cases.
- `git diff --check` passed. Shared Redis tests ran sequentially; unit-only
  tests used no Redis. The dedicated Compose project was removed afterward.

## Review

Agent review follows REVIEW.md: single-key Lua prevents overwrite races and
wrong-type deletion, glob escaping and isRowKey preserve collection boundaries,
application text encoding is retained, and no metadata/HASH system fields are
introduced. Default truncate remains refused and re-keying refusal is unchanged.
Partial progress on concurrent writes/failures is explicit. No human review or
merge approval has been recorded.

## Current-main rebase validation

The original worktree belonged to an unrelated historical lineage. For the PR,
replayed the single change onto current `redis/redis-adbc-driver` main `91064e6`.
Resolved statement option additions without removing current partition/IPC
options. Moved documentation updates to `docs/driver-reference.md`, preserving
the current README structure.

- Unit-only suite: passed all three packages (driver 15.115s).
- A new isolated Redis Software fixture verified active QPF scaling factor 4,
  six Search workers, mnp routing and 32 connections before tests.
- Native library build: `make -C go build` passed after the rebase.
- Broad integration run: `REDIS_URI=redis://localhost:28379/0 go test -json
  -count=1 -p 1 ./...` failed the driver package (116.875s). Captured 801
  passed, 29 skipped and 35 failed test/subtest events before stopping;
  remaining packages were not completed in this attempt. Failures included
  Redis EOF errors and retained-quota control-policy errors. No all-green
  current-main full-suite claim is made.
- Unmodified main `91064e6`: reran TestSQLOuterReferencesFromFrom,
  TestSQLParameterSchema and TestRetainedPartitionAPIEmptyAndBudgetFailure.
  The retained-quota failure reproduced with the same policy error; the two
  other tests passed, so the EOF failures remain intermittent/unresolved.
- Rebased focused regressions: compiled with `go test -race -c`, then ran
  TestAdoptKeyedWrites and TestAdoptInsertCollisionAtomic against the verified
  QPF fixture; both passed, with no skips or race reports. This run completed
  separately from the broad suite and baseline test execution.
- Broad-suite skips included Redis Software protocol ACL restrictions,
  optional profiling fixtures, and environment-specific capabilities. Assertions
  and skip conditions were not changed to hide failures.
- `git diff origin/main --check` passed. The PR is submitted as a draft because
  the current-main broad integration run is not green.

The original evidence above records the actual older base and settings; 4x QPF
was not verified in that earlier run and is not claimed retroactively.
