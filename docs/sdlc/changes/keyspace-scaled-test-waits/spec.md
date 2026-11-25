# Specification

2026-11-25; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

Test-only helpers in `go/keyspace_wait_integration_test.go`:

- `keyspaceWait(ctx, client, base)` returns `base + keys × keyspaceWaitPerKey`, where keys is
  `DBSIZE` summed over a cluster's primaries and `keyspaceWaitPerKey` is 50 µs (measured initial
  scan: 36 µs per key on one Software 8.8 shard). A `DBSIZE` failure returns `base`.
- `clearPrefix(t, client, prefix)` deletes the keys under a glob-escaped prefix on every
  primary with `SCAN … COUNT 1000` under a `keyspaceWait(10 s)` context, reporting errors with
  `t.Error` as before.

Uses:

- `retainedFixture` cleanup calls `clearPrefix` instead of its fixed 10 s / `COUNT 100` SCAN.
- `hashFixture.appIndex` waits `keyspaceWait(10 s)` for the index to finish indexing.
- `TestLiveHashPartitionIndexDefinitionChanged` waits `keyspaceWait(5 s)` for its restored live
  index.
- `newHashFixture` calls `dropStaleLive` after clearing its prefix: every live registration
  whose `KeyPrefix` is exactly the fixture's prefix is removed with `UnregisterLiveHashes`
  (NotFound tolerated). A registration that cannot be removed fails the test with a message
  naming it and asking for a dedicated database. Registrations on other prefixes are untouched.

On a small dedicated database (CI) the waits are unchanged within milliseconds, and no
registration matches. Assertions and skips are unchanged.

Acceptance: after an interrupted run, `TestLiveHashPartitionIndexDefinitionChanged` fails on
main with `live table already registered or capacity reached` and passes with this change. On a
database holding the 1.94M-key TPC-H subset, `TestSnapshotCopyChecklistPrivateNames` (fails on
main: `still indexing`), `TestPartitionManagedCopyOwnership`,
`TestLiveHashPartitionIndexDefinitionChanged` and `TestPendingCleanupAggregateAndPartitionInputs`
pass.
