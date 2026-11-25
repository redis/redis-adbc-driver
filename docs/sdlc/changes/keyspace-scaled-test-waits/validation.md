# Validation

2026-11-25; base `268ccf03`, rebased onto `2ed3b3df` (main) without conflicts (no shared files).
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Environment: isolated Compose project on Redis Software 8.8.0, one shard, active verified QPF
scaling factor 4, `noeviction`, TLS, 4.5 GiB database, through `go/software-setup.sh --exec`
(`SOFTWARE_SHARDS=1`, which also sets the verified retained-control declaration). The database
held the 10% TPC-H subset loaded through the driver: **1,935,303 keys** (`DBSIZE`). Client: macOS
ARM64 host through a Docker published port.

## Rates on that keyspace

Read-only probe, an index on a prefix that matches no key, best of two:

| operation | time | per key |
|---|---:|---:|
| `FT.CREATE … PREFIX 1 it:calib653:` until `FT.INFO` reports it complete | 69.7 / 70.0 s | 36 µs |
| `SCAN 0 MATCH it:none653:* COUNT 100` to completion (19,265 replies) | 5.07 s | 2.6 µs |
| the same with `COUNT 1000` (1,935 replies) | 0.96 s | 0.5 µs |

A new index's initial scan visits the whole keyspace, so a fixed 10 s readiness wait fails from
about 280,000 unrelated keys on this route. `keyspaceWait` allows 50 µs per key (about 1.4× the
measured scan), so 1.94M keys add 97 s. The SCAN cleanup's 10 s budget ran out around 4M keys
with `COUNT 100`; with `COUNT 1000` and the scaled deadline it has more than 100× headroom.

## Failures on main's helpers

Through `go test -overlay` that restores the four helper files from `268ccf03` and removes
`keyspace_wait_integration_test.go`:

- `TestSnapshotCopyChecklistPrivateNames` (in the #631 integration selection, same fixture):
  both subtests fail with `it_hc_private_names_source is still indexing` after 10 s.
- `TestLiveHashPartitionIndexDefinitionChanged` interrupted by `-timeout 20s` (the registration
  waits for the 70 s initial scan), so its `t.Cleanup` did not run. The next run fails at
  `partition_live_hash_integration_test.go:510`: `Already Exists: [redis] live table already
  registered or capacity reached`.

## With the change

`go test -count=1 -p 1 -timeout 45m -v -run 'TestLiveHashPartitionIndexDefinitionChanged$|TestSnapshotCopyChecklistPrivateNames$|TestPartitionManagedCopyOwnership$|TestPendingCleanupAggregateAndPartitionInputs$'`,
starting from the stale registration left above:

| test | result |
|---|---|
| `TestPendingCleanupAggregateAndPartitionInputs` | pass, 5.2 s |
| `TestLiveHashPartitionIndexDefinitionChanged` | pass, 135.2 s: `dropStaleLive` removed the stale registration; registration and the restored index each waited for a 70 s initial scan |
| `TestPartitionManagedCopyOwnership` (adopted / application_index / foreign_index) | pass, 154.2 s (5.4 / 78.5 / 70.3 s) |
| `TestSnapshotCopyChecklistPrivateNames` (both subtests) | pass, 138.8 s (68.8 / 69.9 s) |

`ok … 434.162s`. Unit-only `env -u REDIS_URI go test -count=1 -p 1 ./...` passes; `gofmt -l` on
the changed files is clean. The full live-HASH and retained suites were not rerun on this
1.94M-key fixture (each live registration waits about 70 s here); CI runs them on its fresh
three-shard database, where `keyspaceWait` adds milliseconds and no stale registration matches.

## Not established

- The OOM-at-connect symptom from #653 (a shared database already at `maxmemory`) is a
  precondition this change does not address; the server's OOM error names it.
- Concurrent sessions sharing one database can still collide on fixed names; the suites need a
  dedicated database.
- Multi-shard keyspaces were not measured locally (`keyspaceWait` sums `DBSIZE` over primaries).
