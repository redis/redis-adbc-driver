# Statement snapshots for managed tables: validation

Date: 2026-11-02. Source baseline: `bb83527f305b75ea731696f55f78eb75121bbbc0`.
Status: implemented for draft review; no merge or release implied.
Issue: [#113](https://github.com/redis/redis-adbc-driver/issues/113).
Related: [intent](intent.md), [specification](spec.md), [plan](plan.md),
[usage and reproduction](../../../snapshots.md).

## Environment and reproduction

A separate disposable Redis Software database was created using the current
`go/software-setup.sh` with one shard, authenticated TLS, RAM/noeviction and
active 4x QPF verified by management API readback. The image was
`redislabs/redis:8.2.0-78.18`, runtime Redis 8.6.2 and Search 8.6.11. Six Search
workers, minimum operation workers=4 and routing mnp with 32 connections were
verified. This is correctness evidence, not a performance or failover certificate.
Existing application containers and data were not used.

Host Go 1.27.2 executed the module declaring Go 1.26.10. Native checks used Python
3.13, adbc-driver-manager 1.13.0, PyArrow 26.0.0 and redis-py 8.1.0 in a temporary
virtual environment. TLS CA trust was explicit in both Go and Python raw clients.
Both snapshot suites fully consume and validate results. Ingest tests use checked
Arrow allocators. Operational endpoint details and logs remain private.

The snapshot suites clear the explicitly selected database and its indexes.
Run separately from ordinary integration suites on a disposable database:

```sh
cd go
# Start an isolated Software fixture, then load its generated environment.
source build/test.env
REDIS_SNAPSHOT_URI="$REDIS_URI" go test -race -count=1 -json -run '^TestSnapshot' .
make build
REDIS_SNAPSHOT_URI="$REDIS_URI" python validation/snapshot_validation.py
# Unit-only: unset both Redis URI variables before running.
env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -json ./...
```

## Checks on current main

- Stream retention: 12,001 exact BIGINT values survive concurrent publication
  and subsequent cursor/HASH reloads; the next query sees all new values.
- Nested planning and reads: deterministic barriers cover joins, CTEs, correlated
  and independent subqueries, pushed aggregates and direct HMGET while two table
  updates publish; all reads retain the original root.
- Writes: SQL DML, joined DML, MERGE, RETURNING, CTAS, views, catalog reads,
  schema discovery, TRUNCATE and schema cascade.
- Arrow ingest: all modes, later batch constraint failures and input stream
  errors publish once or preserve the previous root; checked allocators balance.
- Concurrency/recovery: losing writers return SQLSTATE 40001, expired writers
  cannot recreate reclaimed HASHes/indexes, and lost publish replies resolve
  against durable outcomes without replay.
- Ownership: expired/cancelled readers emit no further batch and release pins;
  sixteen retained/current generations reject another writer until reclamation.
- Readiness: missing indexes fail reads; private copied indexes must complete
  initial scanning before DML; final indexes scan immutable rows before publish.
- Namespace/replies: only key positions are scoped, user values are preserved,
  retries do not double-scope keys, and partial Search warnings are rejected.
- Deployment/API boundaries: legacy catalogs remain untouched on rejection;
  snapshots cannot be disabled; context deadlines apply to clients; partition
  production cannot bypass snapshot ownership.
- Native C/ADBC: bound query/schema, late ingest failure and streamed results
  surviving concurrent UPDATE and DROP.

| Command | Result |
| --- | --- |
| Unit-only Go suite on current main | Passed all packages: 888 passing and 602 skipped test/subtest events; integration skips expected |
| Snapshot race suite | Passed 23 test/subtest events, no failures or skips; no enable flag |
| Native shared library build | Passed, Darwin arm64 |
| Native snapshot checks | All three passed, no skips, 9.263 seconds |
| Formatting and diff whitespace | Passed |

The final focused run includes an additional connection capability/partition
boundary test added after the full unit run. The new port preserves current
main's per-execution cancellation transports, detached Arrow results and bounded
cursor cleanup. Live streams retain snapshot ownership; fully materialized
Arrow readers release it after execution. Retained partition reader-only mode
continues to read independent immutable Arrow payloads without opening a managed
catalog; producer APIs explicitly reject this initial format.

## Limits and review

The implementation was originally developed on a checkout with unrelated history,
then transplanted onto the current main baseline above. Only the checks recorded
here describe the final port. No full SQL compatibility suite is claimed green:
ALTER, temporary/foreign/adopted tables, legacy tools and partition production
are deliberately excluded. Snapshot-only tests are separate because the new
mandatory format requires an empty database and old suites assume legacy layouts.

No runtime validation covers Active-Active, Cluster API, multiple shards,
failover, persistence recovery or old binaries sharing this database. Those are
excluded or unverified as documented. There is no migration, downgrade,
multi-statement transaction, row-level delta optimization or throughput claim.
Target-table copies, global traversal costs and fixed limits remain material compatibility/cost risks.

Review covered command scoping/fences, publish ambiguity, writer and reader
lifetimes, cleanup ownership, Arrow buffer ownership, current transport routing,
metadata/ingest entry points and public defaults against REVIEW.md. Human review
is required before merging this material storage change. Issue #113 was read
through GitHub; its old proposal link returned 404. The pre-existing untracked
`.codex/` directory is outside this change.

## Shared-table refinement

Revision baseline: PR #284 commit `2bdd3c784198667fcd983262667a914faba73138`.
The user requested reuse of unchanged tables; the earlier results above describe
the original full-catalog-copy draft, not this subsequent refinement.

Protocol version 2 gives catalog manifests and physical table generations separate
lifetimes. Reservation inherits references atomically. New data is registered
before allocation; publication atomically installs the final reference list.
Reclamation checks all committed and live staging references before claiming
physical data. Creator manifest reclamation does not delete shared data.
Unchanged tables are not copied, row-scanned by validation, or reindexed. DML
still copies target tables; metadata copying, index checks, prefix-filtered SCAN
and Search initial scanning retain catalog/database traversal costs.

Additional complete-result integration checks:

- UPDATE A leaves B's physical prefix/index and exact BIGINT HASH payload
  unchanged. Command inspection checks for copying B or rebuilding its index.
- Twenty subsequent publications and repeated cleanup preserve both current B
  and old A held by a pinned manifest. Releasing the pin permits old A rows/index
  to be deleted without affecting current B.
- DROP B retains its shared rows/index while a reader is pinned, and reclaims
  both after the final manifest reference disappears.
- COMMENT reuses physical data; TRUNCATE replaces only its target, preserving
  old pinned rows and unrelated tables.
- Active writers retain inherited/private generations during competing writes.
  Aborting reclaims private data while preserving pinned base data.
- Direct forged Lua mutations cannot write inherited shared data. Expired
  writers cannot recreate their actual owned HASHes/indexes after reclamation.
- Existing SQL, ingest, nested-read, capacity, cancellation and recovery cases
  continue to pass under the new ownership protocol.

| Check | Shared-table revision result |
| --- | --- |
| Unit-only Go suite | All packages passed; 889 passing / 605 expected skipped test/subtest events |
| Full snapshot race suite | 26 passing events, no failures/skips |
| Native shared-library build | Passed, Darwin arm64 |
| Native snapshot suite | All three passed, no skips, 5.428 seconds |

No throughput benchmark was run. Physical reuse assertions establish elimination
of unrelated row copies/index rebuilds; they are not a quantified speedup claim.
The format version 1 draft is rejected without migration, consistent with the
user's no-upgrade-path instruction. Operational artifacts remain private.

## Row-delta refinement

Revision baseline: shared-table PR commit
`54500f4b37cee0d6aa3603853112f84714ec3f6c`. The preceding sections record historical
checks of earlier drafts. Protocol version 3 adds row-version segments and exact
row-ID directories; there is no migration from either earlier draft format.

Implemented and reviewed against REVIEW.md:

- A one-row UPDATE writes one complete replacement HASH and its directory entry;
  one-row INSERT/DELETE similarly write one new row/tombstone. Physical-command
  inspection verifies no base row copy or old index rebuild. Metadata manifests
  and new segment indexes still incur non-row traversal work.
- Newest versions and tombstones win before WHERE, ORDER BY, OFFSET/LIMIT,
  scalar/grouped/distinct aggregates, joins and correlated queries. Unsafe Search
  and parallel physical-index shortcuts are excluded for overlays.
- NULL field removal and exact BIGINT values survive repeated updates. Row IDs
  beyond 2^53 retain exact directory ordering and direct lookup identities. This
  regression found Lua rounding INCRBY integer replies, which could allocate the
  same physical key twice. Fences now re-read integer counters as exact bulk
  strings within the same operation; the typed Go Redis parser preserves int64.
- Eight segments trigger synchronous compaction. Repeated changes and deletions
  remain correct while old segment directories/indexes/HASHes stay reachable by
  pinned streams. After exhaustion/release and reclamation they disappear.
- Injecting a lost reply after compaction row writes leaves the root unchanged;
  abandoned private data/directories are reclaimed. Missing row/directory cases
  fail closed, including removal of both a replacement and its directory.
- Existing SQL, Arrow modes, writer conflict/fencing, cancellation, capacity,
  nested snapshot barriers and native C/ADBC checks remain in the final suite.
- Directory registration shares the fenced Lua operation with HSET, declaring
  all row/directory/control keys. Read pages, socket I/O and compaction remain
  covered by cancellation/time/staging limits. Tombstones use a reserved field.

Validation uses the same isolated authenticated TLS, one-shard Redis Software
8.6.2 / Search 8.6.11 fixture with verified active 4x QPF. Operational details and
logs stay private. These are complete-value correctness checks, not benchmarks.

| Check | Row-delta result |
| --- | --- |
| Full unit-only Go suite | Passed all packages: 890 passing / 611 expected skipped test/subtest events |
| Snapshot race suite | 33 passing events, no failures or skips |
| Native shared-library build | Passed, Darwin arm64 |
| Native snapshot suite | All five passed, no skips, 6.994 seconds |
| Documentation links and diff whitespace | Passed |

Compaction is foreground and can hit fixed budgets, causing the write to fail
without publication. No background/incremental compactor or visibility-aware
Search pushdown is implemented. Multiple shards, Active-Active, Cluster API,
failover/persistence recovery, historical-binary coexistence and the full legacy
SQL compatibility/CI matrix remain excluded or unverified. The PR remains draft.

## Incremental background-compaction refinement

Revision baseline: delta PR commit `f604973d176a45d53065cd4224e240e35c7738c9`.
Earlier sections are historical draft evidence. Protocol version 4 introduces
durable background jobs; prior draft formats are rejected without migration.

Implemented: automatic connection-owned maintenance pools, one catalog job,
bounded copy batches/checkpoints, lease takeover, phase sealing, index readiness,
latest-manifest prefix grafting and retryable backpressure. Ordinary SQL no
longer runs the compaction row-copy loop. Durable source/output references survive
expired leases; cooperative Close cancels/joins and releases ownership. Failed
workers yield ownership, and background reclamation handles aborted publication
work independently of SQL reads. A short publication gate resolves starvation
while write intent protects active writers before workspace reservation.

Checks inspected/run against the same isolated authenticated TLS Redis Software
8.6.2 / Search 8.6.11, one shard, noeviction and verified active 4x QPF:

- Thirty small writes complete through automatic background compactions while an
  old 2,001-row stream remains open and returns only its original values.
- Hard-bound backpressure performs zero compaction row writes/index creation on
  the foreground SQL path and leaves the root unchanged; writes succeed after
  maintenance reduces the chain.
- Crash-lease expiry plus unrelated reclamation retains a partial output.
  Another store resumes the same output after its checkpoint and rejects stale
  owner writes. Publication preserves newer UPDATE/DELETE/INSERT deltas and the
  latest row sequence. Server phase fences reject writes after copy sealing.
- Compaction copies 100,001 visible rows successfully despite SQL's 100,000-row
  limit; every observed batch is at most 64 rows. Wide-row tests also enforce the
  1 MiB copy-command target, with complete value/length checks.
- Connection-worker cancellation at a copy barrier joins the worker, releases
  its lease and preserves its checkpoint for a replacement worker.
- DROP/TRUNCATE discard obsolete jobs and reclaim their unused output/directories.
- Existing write intent blocks maintenance publication; ready maintenance gates
  new writer admission and publishes after the older writer releases its pin.
- All previous snapshot consistency, exactness, fault and native tests remain in
  the suite. Deterministic protocol fixtures explicitly drive the same worker
  steps; automatic-worker and native tests use production connection lifecycle.

The first automatic run exposed missing output-prefix registration, unreclaimed
maintenance work slots and publication starvation. These were corrected and
retained in direct-read, backpressure, sustained-write and writer-priority tests.
No assertion was weakened to ignore the failures.

| Check | Background-compaction result |
| --- | --- |
| Full unit-only Go suite | All packages passed: 891 passing / 620 expected skipped test/subtest events |
| Snapshot race suite | 43 passing test/subtest events, no failures/skips |
| Native shared-library build | Passed, Darwin arm64 |
| Native snapshot suite | All five passed, no skips, 8.746 seconds |
| Formatting, diff whitespace and snapshot documentation links | Passed |

These are complete-result correctness checks, not throughput/latency benchmarks.
Index creation and metadata/cleanup traversal costs remain. Job-level memory is
limited by server capacity, rather than a total 64 MiB/100,000-row cap; one row
may exceed the soft batch-byte target under the existing hard staging limit.
Multiple shards, failover/persistence certification and the full legacy SQL/CI
matrix remain excluded or unverified. The PR stays draft for human review.

## Compatibility extension

The subsequent user request restores ALTER, temporary objects, copied external
imports, HASH tools and live/retained partition production. It advances to protocol
5 because old draft writers omit these data references. See the
[compatibility validation](../snapshot-compatibility/validation.md) for current
891-unit / 52-race / nine-native evidence. Earlier sections remain historical.
