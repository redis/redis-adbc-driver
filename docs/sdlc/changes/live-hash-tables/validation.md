# Validation and review

Date: 2026-11-18. Source baseline:
`adae05d46d04bbf5be574960b627da7f45cd564b`, with the local live-table changes.
See [intent](intent.md), [specification](spec.md) and [plan](plan.md).

## Checks run

- `make -C go redis-up`: Redis Software 8.8.0 with three shards. The setup and
  `test-software` gates verified active 4x QPF, effective Search workers/routing
  and noeviction before server tests. Operational details remain private.
- `make -C go test-software TEST_ARGS='-race -run TestLive\|TestAdoptHashes\|TestAdoptInsertCollisionAtomic -timeout 5m'`:
  **nine top-level integration tests passed**, zero selected-test skips or
  failures. Six cover live SQL/Redis behavior, exact values, managed isolation,
  index failure/recovery, vanished rows and source-preserving unregister; one
  covers CLI registration and database-option access; two retain managed-copy
  adoption and insert-collision coverage. Packages without matching test names
  reported no tests to run.
- `make -C go test-software TEST_ARGS='-race -run TestSQLCase\|TestSQLHaving\|TestSQLJoins\|TestSQLInformationSchema\|TestSQLScanReadsExactValues -timeout 5m'`:
  **five managed SQL integration tests passed**, zero selected-test skips or
  failures.
- From `go/`, `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`:
  passed. Server-dependent tests skipped as expected; this is unit evidence,
  not independent Redis compatibility evidence.
- `make -C go build adbc-cli`: native shared library and command-line tool built
  successfully with the Makefile's version linker flag.
- Formatting, local documentation links and `git diff --check`: passed.

## Correctness and ownership review

The first NUL-containing source-key regression failed: the historical aggregate
key path omitted that row. The retained regression now passes with exact source
key discovery and bounded Search INKEYS checks, without using `@__key` in an
index expression. Numeric predicates are fully rechecked against fetched rows;
ordering and reducers use fetched values. Large int64 values and string values
containing whitespace, TAG separators and NUL retain their exact values.

SQL and Redis mutations affect the same HASHes. Tests cover TTL/unmapped-field
preservation, insert collisions, prevention of vanished-row resurrection,
type-safe DELETE, source survival after unregister and managed reclamation,
managed/live write isolation, shared-snapshot refusal, schema discovery and CLI
preview behavior. No live HASH prefix enters managed segment references or GC.
Index removal uses no DD. Resume observes an existing index without recreating
it; pending/deleting registration transitions use identity-checked CAS.

This is agent self-review under [REVIEW.md](../../../../REVIEW.md), not human
maintainer approval. The user subsequently requested a PR and source build;
the feature branch is `codex/live-hash-tables`. No issue, external deployment or
release was created.

## Limits

No performance benchmark or speedup claim. The live collection path scans keys
and may materialize results; see [live-mode limits](../../../live-hash-tables.md).
The native library was built, but no new Python/C runtime acceptance run was
performed. The complete Software suite, destructive disposable snapshot suite
and separate Cluster API environment were not run. Unknown create outcomes with
no observable index retain their recovery identity rather than replaying a
possibly delayed operation. Live mode has per-row/partial-write semantics and
does not offer managed statement atomicity or read-modify-write isolation.

## PR preparation on current main

The feature was rebased onto main at
`14b4a494` (merge of #497), excluding the earlier README/CI branch commits.
Conflict resolution preserves main's application-owned unmanaged indexes,
experimental Flex scope, writer scheduling identity and execution hooks. The
removed `FoldedIdentifiers` metadata field is no longer assigned. Documentation
now reflects that unmanaged mode already supports live source SQL writes;
the new mode adds driver ownership of index lifecycle.

After rebasing, the unit-only suite passed again (expected integration skips),
the native macOS ARM64 shared library and CLI rebuilt successfully, and all
14 focused live/adoption/managed SQL integration checks passed again with race
detection on verified Redis Software 4x QPF. No new Python/C runtime acceptance
or full Software/snapshot/Cluster API run is claimed.

Pull request: [#510](https://github.com/redis/redis-adbc-driver/pull/510).

A supplemental selection of `TestUnmanagedLiveSourceWrites` and
`TestUnmanagedEncodingAndMissingIndexDrop` was attempted after rebasing. Its
three server cases skipped because `REDIS_SNAPSHOT_URI` was unset; these cases
clear a disposable snapshot database and were not redirected to the ordinary
fixture. This adds no unmanaged integration evidence beyond the passing unit
and focused live/adoption/managed checks above.

## Automatic live INSERT keys

The follow-up generated-key regression was run before the change and failed
with SQLSTATE 23502 when INSERT omitted `__key`. The live virtual key column now
uses the normal SQL default evaluator with a safely quoted prefix concatenated
with `GEN_RANDOM_UUID()::TEXT`. The volatile function is evaluated for every row;
this default is neither persisted as a public column nor applied to unmanaged
registrations. Explicit NULL still fails, and explicit keys keep their collision
checks.

- `make -C go test-software TEST_ARGS='-race -run TestLive\|TestAdoptInsertCollisionAtomic -timeout 5m'`:
  nine focused integration tests passed, including the CLI case; no selected
  tests skipped. Redis Software 8.8, active verified 4x QPF and noeviction were
  checked by the setup gate before execution.
- `TestLiveGeneratedInsertKeys` checks omitted keys in named and implicit target
  lists, multirow VALUES, DEFAULT, INSERT SELECT, unique canonical UUIDv4 keys,
  RETURNING, quoted/backslash prefixes, explicit keys and NULL, empty-HASH
  refusal, and absence of internal fields in the original source HASHes.
- The unit-only suite passed again with expected server-dependent skips, and
  `make -C go build adbc-cli` rebuilt the native macOS ARM64 driver and CLI.
- Self-review confirmed that only live insert metadata receives the default;
  explicit keys and other table modes preserve their existing behavior.
