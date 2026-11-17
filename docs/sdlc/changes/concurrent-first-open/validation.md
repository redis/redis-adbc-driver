# Validation and review

Date: November 17, 2026. Base: v0.0.28 (`b63a010d`). Source patch is local,
not released; maintainer approval is not recorded here. The PR carries review status.

## Reproduction

The original dbt dependency refresh saw 189 passes, 26 skips and one first-open
setup error with the v0.0.28 release library on Redis Open Source 8.10.3.
Retrying the failed test passed. These are historical compatibility results,
not Redis Software performance measurements.

Before the source fix, the new deterministic
`TestSnapshotOpenConcurrentInitialization` failed both subcases on Redis
Software: stale EXISTS followed by visible Search indexes, and stale EXISTS /
empty index list followed by a nonzero DBSIZE. The errors match the two
startup safety guards. Each subcase retains real server-side catalog state
and validates the competing initializer's row and unchanged catalog.

## Checks

Environment: macOS arm64, Go 1.27.3, Python 3.13.16. A dedicated Redis Software
8.8.0 RAM fixture used one shard, TLS/password authentication, noeviction and
active 4x QPF verified by software-setup.sh. No other fixture was modified.

- Focused regression, previous-format and nonempty-legacy refusal tests passed.
- `go test -race -count=10 -p 1 -run
  '^(TestSnapshotOpenConcurrentInitialization.*|TestSnapshotReaderFormatFencesPreviousStorage|TestSnapshotPartitionLegacyMetadataNotMigrated)$'
  -v .` passed on that Software fixture. Refusal coverage includes a joined
  missing-entry / lost-reply error; it must not become a missing catalog.
- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI -u REDIS_SNAPSHOT_CLUSTER_URI
  go test -count=1 -p 1 ./...` passed. Integration skips in this unit-only run
  do not establish compatibility.
- `make -C go build VERSION=v0.0.28-startup-fix` built the shared library.
- The source-built library passed 25 dbt basic, concurrency and Python-model
  tests (2 expected skips), using four workers on a fresh Redis Open Source
  8.10.3 instance. This is compatibility coverage, not Software qualification.
- Full dbt Software suite using the source-built library: `uv run pytest
  -n 4 -q --tb=short` passed 190 tests with 26 expected skips. It used the
  refreshed dbt Core 1.12.7, ADBC manager 1.13.0 and PyArrow 26.0.1 environment.
- `gofmt` and `git diff --check` passed.

The patch was then rebased cleanly onto main at `292ee254`. The same Go
unit-only command passed after rebasing with Go 1.27.4. Integration evidence
above remains qualification of the patch against v0.0.28.

Cluster API and restricted-backend reader startup were reviewed but not
integration-tested in this change. Native dbt v2 was not rebuilt or rerun.

## Review

Self-review found no outstanding actionable issue in the patch. Catalog
presence is confirmed through the routed client, including when a Cluster API
node observes nonempty storage. The new helper accepts only an exact missing
field error as absence and validates the existing format before continuing.
Every deployment/ACL check and the atomic initialization guard remain active.
Legacy keys/indexes and unknown replies are not retried or rewritten. No SQL
statement, Arrow input, or uncertain initialization mutation is replayed.

A corrected release is still needed before advancing the dbt adapter's release
pin; the published v0.0.28 artifact remains unchanged by this local patch.
