# Flex unmanaged validation

Date: 2026-11-12. Requestor: Albert Wong. Base: `71a8a248`, including managed
Flex snapshot support from #395. Implementation on `codex/flex-unmanaged-tables`. These are local test
results; no release or hosted CI success is claimed.
See [intent](intent.md), [spec](spec.md) and [plan](plan.md).

## Environment

Redis Software **8.2.6-25**, Redis **8.8.0**, Search **8.8.8**, Linux aarch64,
TLS/RESP3 and noeviction. The owned Flex fixture used one primary shard,
BigStore v2, 1 GiB total/256 MiB RAM and a container flash directory. The separate
owned RAM fixture used three primary shards. These are correctness tests,
not cold-storage, performance or high-availability evidence.

`make -C go redis-up` and `go/flex-setup.sh` verified active **4x QPF**, **6 Search
workers**, **mnp routing** and **32 connections** before tests. Independent
management reads verified those effective settings, noeviction, versions and
actual RAM/Flex product configuration for both final fixtures. Operational
identifiers, credentials, certificates and raw logs remain private.

## Regression and behavior

The initial live regression failed for both entry paths: unmanaged adoption
returned the adopted/application-index refusal; enabling unmanaged foreign
indexes returned the foreign-index refusal. Both now pass.

Reads enumerate HASH candidates with SCAN and check application index membership
using singleton FT.SEARCH INKEYS counts. Direct adopted-key lookup also requires
membership; a test recreates an empty SKIPINITIALSCAN index over existing HASHes
and proves that direct lookups and scans omit those unindexed HASHes without
altering them. Registration positively verifies the same membership command.

Live checks cover:

- Adoption and database/connection foreign-index registration; SQL source
  INSERT/UPDATE/DELETE; exact BIGINT values, filtering, aggregates, sorting and
  limits; and mixed managed/unmanaged joins.
- FILTER membership, multiple and overlapping prefixes without duplicate rows,
  indexed non-UTF-8 binary keys, expiry, changed-definition refusal and explicit
  foreign schema refresh through the public utility API.
- Source-field/text encoding and TTL preservation, DROP/GC source/index survival,
  and retained unmanaged ALTER/TRUNCATE refusals.
- Lost successful source-write replies: exactly one mutation executes, subsequent
  rows remain unchanged, the error reports possible partial application, and no
  managed row generation is published.
- Denied SCAN/membership requests, lost membership replies, warnings, impossible
  singleton counts, malformed counts, HASH pipeline transport failures and
  cancellation. Failed readers are terminal. A HASH pipeline failure after
  populated individual replies is propagated rather than accepted as success.

An initial binary-key fixture assumed the application index would include a
NUL-bearing key. Native Search probes returned zero membership for that key in
that filtered fixture on both Software products. The final binary-key case uses
an indexed non-UTF-8 key. This run does not establish NUL-bearing source-key
coverage or diagnose why the native index omitted that fixture key. The driver
must follow native index membership, including omission of unindexed HASHes.

## Checks

Only use disposable databases: the snapshot test harness clears their keys and
Search definitions. Source the private setup environment for the intended
fixture, and run from `go/`.

Broader Flex selection, before the final read-only page-check extraction
and native-error classification regression:

```sh
REDIS_SNAPSHOT_URI="$REDIS_FLEX_URI" go test -race -count=1 -p 1 -v \
  -run '^(TestFlex|TestSnapshotFlex)' -timeout=10m .
```

Passed **24 top-level tests**, **71 passing entries including subtests**,
**5 top-level skips**, in 29.988 s. The skipped existing opt-in checks were
comparative/cold resource profiles, server restart, server failover and a manually
lowered live Search cap. No new skip was introduced to hide a failure.

After extracting a read-only index-definition check for page validation, the
final unmanaged selection was rerun under the race detector:

```sh
REDIS_SNAPSHOT_URI="$REDIS_FLEX_URI" go test -race -count=1 -p 1 -v \
  -run '^TestFlexUnmanaged' -timeout=5m .
```

Passed **5 top-level tests**, **16 passing entries including subtests**, zero
skips, in 5.436 s. Page validation no longer mutates the query's column capability metadata.

RAM regression selection, including the existing unmanaged contract tests:

```sh
REDIS_SNAPSHOT_URI="$REDIS_URI" go test -race -count=1 -p 1 -v \
  -run '^(TestFlexUnmanaged|TestUnmanaged)' -timeout=5m .
```

Passed **10 top-level tests**, **23 passing entries including subtests**, zero
skips, in 6.296 s. Default RAM unmanaged behavior remains covered, including
managed-copy isolation, filtered source DML and unknown-write non-replay.

Additional checks:

- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI -u REDIS_FLEX_URI go test -count=1 -p 1 ./...`:
  passed all packages; endpoint-dependent tests skip in this unit-only run.
- `make -C go build`: passed the native shared-library build with its version
  linker flag.
- `git diff --check`, gofmt, and local documentation target checks: passed.

## Review and limits

Agent self-review followed [REVIEW.md](../../../../REVIEW.md): checked source
ownership, SQL predicate retention, direct-key membership, prefix/byte identity,
FILTER semantics, source expiry/type races, pipeline errors, cancellation,
non-replayable source mutations and metadata-only registration. The source
reader keeps no persisted SCAN cursor and never populates or rebuilds application
indexes. No unresolved actionable finding was identified; this is not human
review or merge approval.

Unmanaged rows remain live. Source writes can partially succeed, and no common
point-in-time view is added to mixed managed/unmanaged queries. Native source
ACLs remain authoritative. Enumeration requires SCAN; membership and source
inspection require FT.SEARCH and FT.INFO. Temporary capability-probe permissions
from the normal stopgap also apply. Membership pipelines are capped at 128
commands, with a 1 MiB request target and 64 MiB single-key ceiling; SCAN COUNT is
a hint, and deduplication memory grows with the number of observed source keys.
Client-side SQL work is not a performance-parity claim.

General unmanaged ALTER, TRUNCATE, Arrow ingestion, managed index changes,
retained write preparation, shared snapshots and live partitions stay excluded.
Managed foreign-copy imports on Flex remain rejected. Full native ADBC, Cloud,
Cluster API, restart/failover and physical-host failures were not validated by
this change. Historical benchmark and transition results remain labeled by their
actual source revisions and settings.

## Cleanup

Removed the task-owned Flex and RAM containers and the task RAM network.
Verified no task-owned containers or task RAM network remained, and removed
their generated endpoint environment files. Pre-existing services were left
in place. Build outputs and raw logs remain private/ignored.

Pull request: [#397](https://github.com/redis/redis-adbc-driver/pull/397).
