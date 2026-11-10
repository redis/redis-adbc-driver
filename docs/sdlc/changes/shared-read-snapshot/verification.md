# Shared read snapshot verification

Date: 2026-11-10. Author: Codex acting on alberttwong's issue/PR request.
Status: source implementation validated; human review and release pending.
Base source: `85509485dbc07b2e2e0ea9e1b8e08bd11a290587`; the PR diff defines the
implementation under test. This is not evidence for a released driver version.

## Environment

macOS arm64, Go 1.27.3 module toolchain, native c-shared ADBC build. Disposable
Redis Software, Redis 8.8.0, three primary shards on one Software node/proxy,
Search enabled, authenticated username/password, TLS with verified test CA and
hostname. `make -C go redis-up` / `software-setup.sh --exec` verified active
4x QPF, effective Search configuration, primary readiness and noeviction before
integration runs. This is the existing single managed-catalog slot boundary,
not distributed independent-shard snapshot evidence. Operational credentials and
private fixture artifacts are excluded from git.

## Recorded checks

- Before implementation, `TestSharedSnapshotMetadataAndSequentialReads` failed
  with unknown connection option `adbc.redis.read_snapshot_action`.
- Full unit-only baseline and implementation runs:
  `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`: passed.
  Redis integration cases skip in these runs; those skips are not Redis evidence.
- Shared snapshot race run, with `REDIS_SNAPSHOT_URI` set by the verified Software
  wrapper: passed. Tests compare complete GetObjects Arrow records, table schemas,
  exact BIGINT values, NULLs and duplicate multiplicities against the pre-update
  generation, then confirm independent ordinary statements observe publications.
- Real process death: a child acquired through native Go ADBC and exited without
  Close. Its untouched two-minute lease expired and ordinary collection removed
  the pin and retired metadata root. Race run including this check passed in
  134.379 seconds. The final broader run passed in 136.735 seconds, including
  actual configured lifetime expiry, blocked-admission progress, compaction,
  partition reader ownership, pin cleanup and independent publication regressions.
  No shortened server expiry was used for the process-death/natural-expiry check.
- Explicit server pin removal and local lifetime cancellation both refused
  buffered streamed batches, later SELECTs and metadata without live fallback.
  Early consumer close/cancellation preserved the parent; connection close
  released it. Parent release preserved an active stream until complete fetch.
- 128 live pins explicitly refused another shared binding without evicting
  existing owners. Unsupported writes, CTE DML, nextval, partitions and attachment
  were refused. String/int retention options validate their finite range.
- `TestSQLRBACSelect` with `REDIS_RBAC_TEST_API` against the disposable authenticated
  Software management API, `-race -count=1 -p 1`: passed (24.304 seconds). A restricted
  user's binding retained old values after DML; REVOKE denied that retained data.
  No snapshot capability was transferred to another connection/principal.
- `make -C go build`: passed, producing the native library with the repository's
  version linker flag. The build's v0.1.0 string is not a published release.
- `shared_snapshot_validation.py` with adbc-driver-manager 1.13.0 and PyArrow
  26.0.1 against that library and verified Software: passed. This independently
  exercises C connection options (including int64 retention), native metadata,
  two table reads, concurrent publications, exact values and generation identity.
- `gofmt` and `git diff --check`: passed.

## Review and remaining gates

Self-review followed [REVIEW.md](../../../../REVIEW.md): correctness/lifecycle,
authority/data ownership, and contract/evidence. The lifetime check is isolated
to shared views so ordinary materialized-reader expiry errors remain unchanged.
Review also removed the binding mutex from ordinary/server acquisition waits; a
deterministic blocked-admission regression covers both acquisition paths. An
intermediate broader run failed because its command unset REDIS_URI, which an
existing partition regression reads directly; the final command sets both URIs.
No server storage protocol change or credential/handle export is introduced.
Self-review is not maintainer approval.

The API is connection-scoped and deliberately rejects attachment/partition use;
there is no cross-process sharing claim. No Flight SQL gateway test was run;
that mode is explicitly unsupported. No released version containing this API
exists in this evidence. Maintainers must review, merge and publish a release;
Airbyte must pin that released version and implement the documented acquire,
metadata/read, failure and release lifecycle before enabling shared_snapshot.
The issue's released-version integration gate therefore remains pending.

## PR CI follow-up: integer AVG expectations

The [amd64 Software CI job](https://github.com/redis/redis-adbc-driver/actions/runs/42856810318/job/129972039473)
failed in `TestSnapshotIndexAggregatesMergeVisibleGroups`. Its expected strings
predated #366's exact NUMERIC integer-AVG contract. Reproduced on the verified
local Software fixture with `-race`: actual averages were correctly scale-16
NUMERIC values, rather than the older unscaled representations. Updated the
three grouped expectations and the fractional scalar expectation, and asserted
precision 76 / scale 16 for every grouped AVG, including the NULL group. Kept
exact values, multiplicities, visibility and zero-HASH-fetch assertions intact.
The focused regression passed with `-race -count=1 -p 1` (2.087 seconds).
