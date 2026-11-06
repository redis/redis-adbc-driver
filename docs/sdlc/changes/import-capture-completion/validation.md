# Import capture completion validation

Date: 2026-11-06. Production source: `a9e413b0`, rebased onto `617e282a`.
Later changes in this PR add these records only. See [intent](intent.md),
[specification](spec.md) and [plan](plan.md).

## Environment

Owned disposable Redis Software fixtures only. The single-endpoint fixture runs
Redis 8.8.0 with three shards; `make -C go redis-up` and the `software-setup.sh
--exec` gate verify active scaling factor 4, six Search workers, `mnp` routing,
32 connections and noeviction before testing. The Cluster API orchestrator verifies
three Software nodes, three primaries and three replicas, TLS, AOF persistence,
noeviction and effective QPF4 before running its selection. Cluster binary:
`GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c`.
Operational state, credentials, licenses and raw logs remain private.

## Results

- Unit-only: `env -u REDIS_URI -u REDIS_CLUSTER_URI -u REDIS_SNAPSHOT_URI
  -u REDIS_SNAPSHOT_CLUSTER_URI go test -count=1 -p 1 -timeout 10m ./...`
  passes. Redis integration tests skip in this unit-only run.
- Snapshot imports: run through `software-setup.sh --exec`, set
  `REDIS_SNAPSHOT_URI` and `REDIS_ADMIN_URI` to the verified fixture URI, unset
  `REDIS_URI` and `REDIS_CLUSTER_URI`, then run `go test -race -count=1 -p 1
  -timeout 15m -v -run
  '^TestSnapshot(Import|ForeignImport|HashImport|Metadata.*Publication|.*Publication.*Metadata)' .`.
  Pass: 31 passing test/subtest events, no failures, 11 skips, 85.422 seconds.
  Nine skips belong to Cluster-only publication cases exercised below. Two
  permission-preflight cases require separate principals denying journal HDEL
  or ZREM and remain unexecuted; no permission coverage claim is made for them.
- Managed import compatibility, separately with the ordinary `REDIS_URI` and no
  snapshot-test URI: `software-setup.sh --exec go test -race -count=1 -p 1
  -timeout 10m -v -run '^TestManaged.*(Import|Inspection)' .` passes, 5.233 seconds,
  no failures or skips. This covers independent index policy, source reports,
  refresh identity, Arrow replacement and provenance joins.
- Cluster API: use `scripts/snapshot_software_cluster.py --action test` with
  `--test-pattern '^TestSnapshotCluster(.*ImportCapture|ImportsAcrossPrimaries|MetadataDirectoryAndRekey|Publication)'`.
  Pass: 11 top-level tests / 26 passing test/subtest events, no failures or skips.
  This includes the metadata-publication proof touched by conflict resolution.
- Red/green: copy only `snapshot_import_capture_admission_test.go` onto unmodified
  main `617e282a`, compile and run
  `^TestSnapshotClusterIncompleteImportCaptureCannotEmitOrPublish$` on the same
  verified Cluster fixture. All five subcases fail as expected: journal output,
  plain batch output, raw HASH output, seal and publish admit incomplete capture.
  The rebased implementation passes all five without mutating output/input/root
  on refusal. This is a protocol admission regression; the existing normal Go
  source loop already finishes iteration before applying output.
- The first exploratory standalone selection failed its source-key partition
  helper because `REDIS_ADMIN_URI` was missing. The corrected run above passes
  the complete source-key identity and late-projection cases. No assertion or
  production behavior was changed for that fixture error.

## Review

Codex reviewed all eight production/test files across correctness/lifecycle,
ownership and intent/evidence passes. The sole rebase conflict composed two Lua
proof helpers; both checks remain present and the publication-integrity cases
pass. Capture remains reference-owned private statement state. No public process
resume or row-ID reservation experiment is included. No actionable finding was
identified in this focused review. This is agent review, not human approval.

The PR remains draft. Exact-head hosted CI and fixture cleanup are verified
separately during delivery; local results do not substitute for hosted checks.
