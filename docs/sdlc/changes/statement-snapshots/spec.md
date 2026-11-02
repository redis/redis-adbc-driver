# Statement snapshots for managed tables: specification

Date: 2026-11-02. Source baseline: `bb83527f305b75ea731696f55f78eb75121bbbc0`.
Status: implemented in the working tree; mandatory bounded catalog format.
Requestor: Albert Wong. Issue: [#113](https://github.com/redis/redis-adbc-driver/issues/113).
Related: [intent](intent.md), [plan](plan.md), [validation](validation.md),
[user documentation](../../../snapshots.md).

## Observable contract

Each supported SELECT atomically acquires the current catalog generation and a
pin before resolving stored relations. Its schemas, views, physical HASH prefixes,
indexes, exactness metadata, subqueries, joins, CTEs, direct lookups, aggregates
and worker scans use that generation. A stream retains it through its last storage
read and validates ownership before exposing each Arrow batch. Already-built
Arrow buffers own their data independently.

Supported writers stage their complete SQL statement or Arrow input in a private
catalog generation. One atomic publication switches visibility. SQL scripts and
bound parameter executions use separate snapshots per executed statement.
RETURNING is exposed after publication. No multi-statement transaction or rollback
guarantee is introduced.

## Initial implementation boundary

All managed-table connections enable snapshots without configuration. The
`adbc.redis.statement_snapshots` capability reports true; attempting to disable
it fails. First open requires an empty database and no existing Search indexes.
Existing unversioned catalogs fail without modification. There is no upgrade
path, as explicitly directed by the user; old executables and raw clients must
be fenced operationally during deployment.

Permanent managed tables, schemas and views are supported. SQL DML, joined DML,
MERGE, CTAS, table/view/schema creation and removal, TRUNCATE, COMMENT and all
Arrow ingestion modes publish private generations. ALTER TABLE, temporary
relations, adopted/foreign/application indexes and legacy HASH tools are rejected.

Writes copy the metadata manifest and share immutable row-version segments.
DML targets receive an empty current segment before planning captures logical
row keys. INSERT appends new rows; UPDATE writes complete replacement images;
DELETE writes tombstones. Arrow append ingestion stages its complete stream in
one segment. Unchanged rows and old segment indexes are not copied or rebuilt.
Bounded synchronous compaction replaces a segment chain when needed.

## Storage and publication

All control records, metadata and row keys share `{meta}`. Index names also
include this tag. Cluster API connections, non-primary endpoints, observed
Active-Active CRDT configurations and eviction policies other than noeviction
are rejected. A single endpoint with working hash-tag routing is required. The
verified topology is one Redis Software shard with Search and active 4x QPF.

The control hash `adbc:{meta}:snapshot:catalog` contains:

| Field | Contents |
| --- | --- |
| `format` | Protocol version `3`; prior draft versions 1 and 2 are rejected |
| `root` | Unique committed catalog generation ID |
| `w:<generation>` | Staging owner token, base root and lease expiry; committed outcome; or deletion claim |
| `p:<token>` | Reader/writer base root and lease expiration |
| `r:<manifest>` | JSON array of referenced physical segment IDs |
| `d:<segment>` | Original staging owner and live/deleting state; lifetime independent of creator manifest |

Legacy-shaped metadata keys are scoped to
`adbc:{meta}:snapshot:workspace:<generation>:` at the client command boundary.
Only key positions are rewritten; values, SQL literals, set members and script
bodies are preserved. Rows live under
`adbc:{meta}:snapshot:rows:<table-generation>:`. Index names identify the same
physical segment. Multiple catalog manifests may reference that same
immutable prefix/index. IDs and physical names are never reused.

A staging manifest atomically inherits its base manifest's segment references
when reserved. Every new segment is registered and added to its staging
references before allocation. Metadata is copied with DUMP/RESTORE. Table
metadata records `snapshot_overlay`, prior `snapshot_segments` in oldest-to-newest
order, and the current prefix/index plus `snapshot_rows`. Segment row counts are
physical versions, including tombstones, rather than logical SQL row counts.

Every physical segment has a sorted-set row directory at
`adbc:{meta}:snapshot:members:<segment>`. Scores are zero; positive int64 row IDs
are padded to 20 decimal digits, so ZRANGE order is exact above 2^53. Each fenced
row HSET atomically registers its directory entry with ZADD. Directory keys are
explicitly declared alongside row/control keys in the Lua call. Writer owner,
staging state, lease and physical-data ownership checks precede both writes.
External mutation remains unsupported. Reserved `__` HASH fields cannot collide
with user columns. Tombstones store row identity plus the internal deletion flag.

Overlay scans merge up to eight immutable directories in bounded pages (256 IDs
per segment). Equal IDs advance together and the newest segment wins. Winning
HASH fields are fetched authoritatively; tombstones suppress older versions.
Only then are predicates, SQL grouping/aggregates, sorting, OFFSET and LIMIT
applied. Direct row-ID lookups resolve newest-to-oldest without enumerating all
rows. Missing rows advertised by directories, mismatched identities or truncated
published directories fail reads rather than exposing older versions. Streaming
keeps bounded pages and the ordinary snapshot pin/deadline lifetime.

Unsafe physical Search predicate, sort/LIMIT, aggregate, distinct, grouping and
parallel partition shortcuts are disabled on overlays. Joins and semi-joins use
visible rows. Non-overlay base tables retain their existing Search paths.
Explicit ordering may materialize; this is a read-cost tradeoff, not a claim that
existing pushdown performance is preserved.

A table has at most eight segments. The next write compacts visible rows into a
fresh current segment, discarding tombstones and obsolete versions, then applies
its changes there. Compaction preserves exact stored values and row IDs and does
not modify old segments. It is synchronous, bounded by existing staging/row/time
budgets, and failure leaves the published root unchanged. Large tables can exceed
these budgets and block chain extension; incremental/background compaction and
configurable limits are deferred. DROP removes a descriptor. TRUNCATE/replace
allocate fresh storage and clear segment history. Metadata-only writes reuse
segments. Read-only source relations remain shared.

New private segment indexes are created and validated before execution. Fresh
indexes scan final immutable current-segment HASHes before publication; old
indexes are never rebuilt. Directory cardinality is compared to index document
counts, and indexing failures prevent publication. Expected counts are stored
in each immutable descriptor. Acquisition checks every segment's directory,
index identity, row count, indexing state and failure count. Search warning
checks remain in the non-overlay read paths. Search initial scans and metadata
or cleanup scans can still traverse the database, so change-sized row/index
work is not a constant-time or throughput guarantee.

Before publication, final descriptors are enumerated into a deduplicated table
reference list. Publication checks those data records are still live, compares
the current root with the pinned base, and atomically writes the new root,
committed outcome and final reference list. Dropped/superseded references are
removed only at this publication point. Any intervening publication causes SQLSTATE
40001, including an unrelated-table change. There is no automatic SQL/input
replay or rebase. Publication replies lost to I/O or cancellation are resolved
through the stored operation outcome using an independent bounded context.
Unknown outcomes explicitly prohibit blind retry. Outcomes last until generation
reclamation; they are not permanent history.

## Pin lifetime and reclamation

Pin acquisition and root selection occur in one Lua operation. Leases use Redis
TIME, last two minutes, and renew every 30 seconds. Expired pins cannot be
resurrected. Lost ownership fails the statement/stream. Cancellation, exhaustion,
Release and handle closure release ownership idempotently; cancellation also
stops heartbeat renewal without requiring another Next call.

Reclamation has separate manifest and physical-data phases. An atomic manifest
claim excludes current roots, pinned roots and live staging writers. Claimed
manifest metadata is deleted, then its outcome/reference record is removed.

An atomic data claim gathers references from every committed manifest and every
unexpired staging manifest. Only an unreferenced table generation can transition
to deleting. New references cannot be published to deleting data. The reaper then
drops only that generation's indexes, row keys and directory, then removes its
data record.
A physical segment can outlive its creator manifest. Unpinned retired manifests
conservatively retain data until their own reclamation; pins keep entire
manifests reachable. Active writers protect inherited and newly allocated data.
Aborted/expired writers cannot renew or mutate those generations.

Both phases are resumable and idempotent, with a five-second budget per
acquisition and one manifest/data cleanup candidate per attempt. Failed cleanup
retains its record for retry. Legacy sweep/rekey/temporary cleanup does not
operate on this storage format.

Fixed limits: eight segments per table, 16 registered catalog manifests, 128 active pins, 64 MiB conservative
staged command-data accounting and 100,000 row HSET operations per write, and
15 minutes per SQL statement/ingestion including streamed consumption. Changed rows, tombstones
and any compaction copies consume the write budget. Limits fail without
publication; no capacity policy deletes pinned data. The command-data budget
does not measure physical index memory or all Redis overhead.

## Failure limits and evidence

Failed copy, cast, constraint, ingestion batch or index validation leaves the
current root unchanged. Cancellation racing publication can observe a committed
outcome and must not claim rollback. Cleanup follows the recorded state, never
guessing that a committed generation is abandoned.

Durability remains the deployment's Redis persistence/replication guarantee.
This version makes no additional disaster-recovery, cross-slot or Active-Active
promise. Live migration, downgrade, failover certification and scale/performance
benchmarks are outside this first runtime slice. Exact validation performed and
its limits are recorded in [validation.md](validation.md).
