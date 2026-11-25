# Implementation and validation

Date: 2026-11-24. Status: local verification complete; human review pending.
See [intent](intent.md) and [spec](spec.md).

1. Confirm the cause: `store.search` sends FT.* to `MasterForKey`'s node
   client, go-redis v9.24.0 node clients do not inherit cluster hooks, and
   `ClusterClient` has no dial hook chain.
2. Change `rawClient` in `go/sql_integration_test.go` (see [spec](spec.md)).
   No driver code changes.
3. Add `TestRawClientHooksSeeEachCommandOnce`
   (`go/rawclient_hooks_integration_test.go`).
4. Run the 54 tests that pass hooks to `rawClient`, directly or through
   `sharedScanPlan`, `cleanupSnapshotPlan`, `readinessScanPlan`,
   `testSnapshotAlterReadinessCase` or `frozenPlanStore`, before and after.

Environment: macOS arm64 Docker host, Go 1.27.4, go-redis v9.24.0. Redis
Software 8.2.6-58 (Redis 8.8.0 database, 3 shards, Search 8.8.8) with the
4x QPF gate (scaling factor 4, 6 Search workers, mnp, 32 connections) verified
in proxy and in Cluster API mode. Redis Open Source 8.6.2: three TLS primaries
in one container. Cluster runs used linux/arm64 test binaries inside the
fixture containers.

| Endpoint | Before | After |
|---|---|---|
| Software, single-endpoint proxy | not rerun (CI baseline passes) | 46 pass, 8 skip |
| Software, `oss_cluster` on | 31 pass, **15 fail**, 8 skip | 46 pass, 8 skip |
| Open Source 8.6.2 cluster | 31 pass, **15 fail**, 8 skip | 46 pass, 8 skip |

The 15 failures before the fix: `TestSQLSnapshotCleanupDeniedRecovery`,
`TestSQLAlterLeftoverAttribute`, `TestSQLExactCountDistinct`,
`TestSQLDistinctPagesAndShards`, `TestSQLLeftoverIndexOwnership`,
`TestAlterWaitsBeforeMetadataCommit`, `TestRangeReadinessSchemaConvergence`,
`TestTruncateAttributeReadinessRetry`, `TestBoundedScanRejectsBuildingIndex`,
`TestBoundedScanReadinessTransitions`, `TestOrdinaryBuildingIndexUsesHashFallback`,
`TestSnapshotIndexReadinessTerminalErrors`, `TestSnapshotTruncateIndexReadiness`,
`TestSnapshotCandidateBoundedWindows` and
`TestSnapshotNumericNullOrderUnavailableProof`. Each is a fault or observer hook
that never saw an FT.* command.

Skips: five opt-in performance profiles (`TestClickBenchExecutionProfile`,
`TestQ19Profile`, `TestUnmanagedAggregateExecutionProfile`,
`TestUnmanagedProjectionConcurrencyProfile`, `TestUnmanagedProjectionSparseProfile`)
were not run. Three tests need `REDIS_SNAPSHOT_URI`
(`TestUnmanagedAggregateInputVectors`, `TestUnmanagedWideFullAggregateProjection`,
`TestUnmanagedProjectionReadLiveChangeBeforeAllNullFallbackSoftware`); they
passed on the disposable Software proxy and were not run on the clusters.

`TestRawClientHooksSeeEachCommandOnce`, built with `-overlay` against each
harness variant, on both clusters:

- original harness: fails, FT._LIST and WATCH seen 0 times, GET and SET once;
- hooks added to nodes without the marker: fails, GET 3, SET 3, EXISTS 2;
- this change: passes, and passes on the Software proxy.

`gofmt` is clean for the changed files. `go vet` reports only the existing
copylocks warning in `live_ingest_creation_integration_test.go`.
The unit-only pass (`env -u REDIS_URI go test -count=1 -p 1 ./...`) passed in
all seven packages with tests. The full Go suite was not run on a cluster.
Agent review is not maintainer approval.
