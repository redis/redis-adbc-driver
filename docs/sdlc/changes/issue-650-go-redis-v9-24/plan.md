# Plan and evidence

Date: 2026-11-24. Source: `ea16062c`. See [intent](intent.md), [spec](spec.md)
and [#650](https://github.com/redis/redis-adbc-driver/issues/650).

1. `go get github.com/redis/go-redis/v9@v9.24.0` and `go mod tidy` in `go/`.
2. Review the v9.24.0 behaviour changes against the driver's use
   ([spec](spec.md)).
3. Keep planner probes outside statement accounting
   (`aggregate_compact_integer.go`). Copy the new shared settings in
   `clusterOptions` (`database.go`).
4. Regressions on the modeled WATCH/MULTI peer (`execution_pool_state_test.go`):
   the shared pool on single and cluster clients, for both
   redis/go-redis#4315 cases. Update the two execution-pool tests that relied
   on v9.23.0 keeping the refused socket. Probe regression with a hanging PING
   (`aggregate_compact_integer_integration_test.go`). URI and option-copy
   tests (`timeout_test.go`).
5. Document the Cluster API `+cluster|shards` permission, `max_active_conns`
   and the newly parsed URI parameters (`docs/driver-reference.md`,
   `docs/retained-partition-results.md`). Grant it in the ACL tests and
   partition TLS scripts that hold users to the documented profiles. Record
   the go-redis#4315 adoption
   (`docs/upstream-submissions/performance-2026-11-14.md`).
6. Run unit-only, the live Software suite, and Cluster API topology checks.

## Evidence

Local macOS arm64, Go 1.27.4, 2026-11-24 (US Pacific). Fixture: dedicated
`make -C go redis-up` Redis Software 8.2.6-58 node, database Redis 8.8.0, three
shards, TLS, `noeviction`, verified through the management API: QPF active
with scaling factor 4, six Search workers, `mnp` routing, 32 connections.
Logs stay local; they are not durable CI artifacts.

| Check | Result |
|---|---|
| `env -u REDIS_URI go test -count=1 -p 1 ./...` | All packages pass |
| `make -C go test-software` (dedicated compose project and ports) | Exit 0 in 22.7 min. Top level: 1,567 pass, 0 fail, 442 skip (with subtests: 3,412 / 0 / 878). Skips: 297 need `REDIS_SNAPSHOT_URI` (next row), 33 `REDIS_SNAPSHOT_CLUSTER_URI`, 17 ACL management over the data path (Software refuses `ACL`), 7 `REDIS_FLEX_URI`; 88 are opt-in profiles, orchestrator gates and owned Software API gates |
| Snapshot suite, `REDIS_SNAPSHOT_URI=$REDIS_URI software-setup.sh --exec ci_go_suites.py run --suite snapshot`, 4 shards under `-race` | All four exit 0 in 14.2 min; 472 tests: 404 pass, 0 fail, 68 skip (37 Cluster API, 17 opt-in profiles, 14 owned reader-lease model, ACL-management or process-helper gates) |
| Same database switched to the OSS Cluster API (`all-master-shards`, external address 127.0.0.1), probe run in the container | go-redis loads topology with one `CLUSTER SHARDS` (one shard, slots 0-16383, `tls-port` 12000); no `CLUSTER SLOTS` |
| `^TestSnapshotCluster` there | 35 pass, 2 fail, 4 skip. The two failures need three routed endpoints (`requires three verified primaries and multiple routed endpoints`); they fail the same way with main's code and v9.23.0. The four skips need the RDB/recovery orchestrator |
| 105 transaction, lease, rekey, rename, adopt, alter, tag, view, matview, ACL and execution-pool tests there, with `REDIS_URI` on the Cluster API | 90 pass, 2 fail, 12 skip. `TestSQLSnapshotCleanupDeniedRecovery` and `TestSQLAlterLeftoverAttribute` fail identically with main's code and v9.23.0: their fault hooks sit on the cluster client, not on the node clients that run search commands |
| Redis Open Source 8.6.2 3-shard TLS cluster, `TestACL.*` and both retained-partition ACL tests | 16 pass. With the old `+cluster\|slots +command` rules, `TestACLDriverUser` and `TestRetainedPartitionAPIIndependentProcessAndMinimalACL` fail: `ACL log for …: [command cluster\|shards]` |
| Same cluster, the 105-test subset | 98 pass, 2 fail (the same two, identical with main's code and v9.23.0), 4 skip |
| Raw go-redis probe on that cluster | Full user: `CLUSTER SHARDS` only. User with only `+cluster\|slots`: `CLUSTER SHARDS` refused (`NOPERM`), then `CLUSTER SLOTS`; all three primaries routed |
| `uv run … python -m unittest discover -s scripts -p 'test_*_partition_tls.py'` | 49 tests pass |

Failing before the change, on the modeled peer (unit tests):

- go-redis v9.23.0 (main's `go.mod` and `database.go` overlaid):
  `TestSharedPoolRetiresUncleanTransactionSocket` fails in all four cases,
  `later borrower inherited WATCH 1` and `later borrower inherited MULTI QUEUED`.
  `TestCompactTupleProbeOutsideExecution` passes.
- v9.24.0 without the probe change: `TestCompactTupleProbeOutsideExecution`
  fails, `planner probes counted as 6 statement commands` and
  `timed-out planner probe discarded the execution pool`.

Not run: Python ADBC validation (`make -C go validate`), a 3-node replicated
Software Cluster API fixture (needs a private license), Redis Cloud, Flex and
linux/amd64. `go vet` reports one existing copylocks warning in
`live_ingest_creation_integration_test.go`, and `gofmt -l` lists
`infoschema_test.go`; neither file changed here. No human review; no release.
