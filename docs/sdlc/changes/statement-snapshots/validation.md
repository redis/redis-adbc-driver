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
