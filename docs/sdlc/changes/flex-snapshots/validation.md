# Flex statement-snapshot validation

Date: 2026-11-12. Requestor: Albert Wong. Source baseline: `a8de1c3c`,
with the local changes described by [intent](intent.md), [spec](spec.md) and
[plan](plan.md). These are current local results, not hosted CI or a release.
At initial validation, no issue or PR had been created for this change.

## Environment

Redis Software **8.2.6-25**, Redis **8.8.0**, Search **8.8.8**, Linux aarch64,
TLS/RESP3, noeviction. The disposable Flex fixture used BigStore v2, one primary
shard, 1 GiB total and 256 MiB RAM. Its flash path was a container directory;
this was a correctness run, not cold-SSD or performance evidence.

`make -C go redis-up` initialized/verified Software. A separate owned Compose
project was used for the final three-shard RAM regression fixture.
`go/flex-setup.sh` initialized/verified the disposable Software Flex fixture.
Before tests, both setup paths required active **4x QPF**, **6 Search workers**,
**mnp routing** and **32 connections**. A separate management read also verified
those effective settings, noeviction and BigStore on the Flex fixture.
Operational identifiers, endpoints, credentials, certificates and raw logs are
private and are not included here.

## Regression and implementation evidence

Before the fix, `TestSnapshotFlexLifecycle` failed at Open with the existing
Flex snapshot `NotImplemented` rejection. During implementation the new
single-baseline directory path exposed a missing full-predicate recheck (257
rows instead of 3); the final implementation retains the original predicate
for both baselines and overlays. Arrow replace also reproduced a legacy prefix
deletion attempt against shared snapshot data; snapshot DROP/replace now leave
row reclamation to the reference graph.

The current tests cover SQL create/insert/update/delete, exact BIGINT/DECIMAL,
NULL/NaN/infinity, filtering, sorting, grouped aggregates and limits; Arrow
create/append/replace and failed input atomicity; streamed old-generation reads;
publication CAS/rebase/conflicts and lost replies; index notifications,
malformed/missing row images, permission/transport/warning failures; cancellation
and expired pins; compaction and missing-index recovery; TRUNCATE and identity
restart; staged rekey abort; and live partitions retained across updates,
rename/rekey, TRUNCATE and DROP.

Real socket disconnect/timeout/cancellation tests fault later directory/HASH
reads after 256 rows have been delivered. Each test requires an explicit terminal
error and validates all 25,000 rows on a new whole-statement retry. Boundary
checks read real rows at hidden IDs `2^53-1` and `2^53`, reject a further insert
without advancing the counter, and verify atomic concurrent allocation inside
a private snapshot writer. A separate test proves later inserts do not change
a pinned older root's row counter.

The earlier mutable-source stopgap tests now use pinned snapshot metadata and
COW counter updates. Their consistency assertions follow immutable snapshots;
low-level bounded-reader reply/cap tests remain. Application HASH ownership
checks use prefixes outside the driver's immutable managed physical namespace.
No new Flex-specific skip or weakened failure assertion hides a regression.

## Commands and results

Run these only against disposable fixtures, after the setup verification above.
The snapshot harness clears the selected database and its Search definitions.
Source the private setup-generated environment for the intended fixture first.

| Check | Result |
|---|---|
| `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI -u REDIS_FLEX_URI go test -count=1 -p 1 ./...` | Passed all packages; Redis-dependent tests skip without their endpoint variables |
| Flex: `REDIS_SNAPSHOT_URI="$REDIS_FLEX_URI" go test -race -count=1 -p 1 -v -run '^(TestFlex\|TestSnapshotFlex)' -timeout=10m .` | Passed: 19 top-level tests, 56 passing entries including subtests, 5 top-level skips; 36.653 s |
| RAM: `REDIS_SNAPSHOT_URI="$REDIS_URI" go test -race -count=1 -p 1 -v -run '^(TestSnapshotFlex\|TestSnapshotIndexProofPermissionFallback\|TestSnapshotFinalIndexAvoidsUnrelatedDatabaseScan\|TestSnapshotCompactionLegacyUnindexedCopy\|TestSnapshotCompactionPreindexInterrupted\|TestSnapshotIndependentPublication\|TestSnapshotArrowIngest\|TestSnapshotConflictsAndExpiredWriterFence\|TestSnapshotManagedStatements\|TestSnapshotLostPublicationReply\|TestSnapshotPrivateReadsAndSealedIndexReadiness)' -timeout=15m .` | Passed: 27 top-level tests, 66 passing entries including subtests, zero skips; 40.548 s |
| Final typed row-limit error classification on RAM: `go test -race -count=1 -p 1 -run '^TestSnapshotFlexRowCounterIsolationAndLimit$' -timeout=2m .` with the RAM snapshot endpoint | Passed |
| `make -C go build` | Passed; built the native shared library with the Makefile version linker flag |
| `git diff --check` and local Markdown target checks | Passed |

The five Flex skips were `TestFlexComparativeProfile`,
`TestFlexColdResourceProfile`, `TestFlexServerRestart`,
`TestFlexServerFailover` and `TestFlexLiveSearchCap`: their additional opt-in
benchmark/operator configuration was not supplied. No fresh restart, failover,
physical-host outage, cold-storage or lowered-server-cap result is claimed.
The historical 2026-11-05 evidence remains labeled by its original source and
settings. Full native ADBC validation and Cloud compatibility were not rerun.
General ALTER, CREATE INDEX, adoption/application/foreign indexes and materialized
views retain their stopgap exclusions. Flex publication requires FT.SEARCH;
RAM's write-only initial-scan fallback remains separately tested and unchanged.

## Review

Agent self-review followed [REVIEW.md](../../../../REVIEW.md): checked SQL
predicate retention, version/tombstone visibility, row-ID precision and COW,
non-replayable index notification failures, publication permission/error
classification, private compaction retirement with retained sources/candidates,
and reference-owned DROP/replace cleanup. No unresolved actionable finding was
identified in these passes. This is not human approval or evidence of a merge.

The primary operational tradeoff is client-side scanning/filtering and sorting:
this establishes stopgap correctness, not performance parity. Current live
coverage is one-shard Software Flex; other topologies and fault transitions need
fresh evidence before extending the compatibility claim.

## Cleanup

Removed the task-owned RAM and Flex containers and the task RAM network.
Verified no task-owned containers or task RAM network remained. Removed their
generated endpoint environment files; pre-existing services were left in place.
The built shared library remains under ignored `go/build/`.

## PR preparation

Rebased cleanly onto `0bc9e855` (the unmanaged-table change). The unit-only
all-package suite and native shared-library build passed again on the rebased
source. The live Software RAM
and Flex results above remain evidence from the original `a8de1c3c` baseline;
they were not rerun after this rebase.

Pull request: [#395](https://github.com/redis/redis-adbc-driver/pull/395).
