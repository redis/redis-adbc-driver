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
Arrow ingestion modes publish private generations. ALTER, temporary objects,
managed copies of external HASHes/indexes, HASH tools and partition production
are supported by the [compatibility extension](../snapshot-compatibility/spec.md).
External sources require explicit refreshes; their publication is atomic, while
capture is not a transaction with application writers.

Writes copy the metadata manifest and share immutable row-version segments.
DML targets receive an empty current segment before planning captures logical
row keys. INSERT appends new rows; UPDATE writes complete replacement images;
DELETE writes tombstones. Arrow append ingestion stages its complete stream in
one segment. Unchanged rows and old segment indexes are not copied or rebuilt.
Durable background jobs compact prefixes in bounded batches. Foreground writes
apply retryable backpressure at the hard segment bound.

## Storage and publication

All control records, metadata and row keys share `{meta}`. Index names also
include this tag. Cluster API connections, non-primary endpoints, observed
Active-Active CRDT configurations and eviction policies other than noeviction
are rejected. A single endpoint with working hash-tag routing is required. The
verified topology is one Redis Software shard with Search and active 4x QPF.

The control hash `adbc:{meta}:snapshot:catalog` contains:

| Field | Contents |
| --- | --- |
| `format` | Protocol version `5`; prior draft versions 1–4 are rejected |
| `root` | Unique committed catalog generation ID |
| `w:<generation>` | Staging owner token/base/lease; compacting owner/lease/publication gate; committed outcome; or deletion claim |
| `p:<token>` | Reader/writer base root, write intent and lease expiration |
| `compaction` | One durable job: source descriptors, output ID, exact checkpoint, phase, owner token and last recorded error |
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

Automatic maintenance starts at four segments; eight remains the hard bound.
Ordinary writes allocate a delta and never copy existing rows for compaction.
Each managed connection owns a separate maintenance pool/controller. A catalog
has one durable job, leased to one controller, while other connections can take
over after cooperative release or two-minute crash-lease expiry. Source/output
references remain protected even while that lease is expired. Closing a
connection cancels/joins its controller before closing statement resources;
without open managed connections, progress waits for the next Open.

Copy steps seek each immutable source directory past the persisted exact row-ID
checkpoint using ZLEXCOUNT. A step processes at most 64 merged row IDs and targets
1 MiB of output command data. One oversized row can exceed that soft target under
the existing 64 MiB hard staging budget; source prefetch is row-count bounded.
Progress is checkpointed only after writes succeed. Lost replies cause safe
idempotent recopy of immutable rows. Each batch resets its staging counters, so
jobs can exceed SQL's total 100,000-row and 15-minute limits. Two-second worker
I/O contexts and separately bounded reclamation limit maintenance operations.

Copy completion durably moves the job to index phase. The server fence rejects
further row writes to sealed output, even from the same lease owner. Only index
creation is permitted in index phase. Once that immutable output index validates,
installation copies the latest metadata manifest and replaces exactly the saved
source prefix with the compacted segment, retaining newer deltas, metadata and
row sequence. It registers the output/current physical prefix and index in the
new manifest. DROP/TRUNCATE/replacement or another incompatible lineage discards
the job. Publication ambiguity is resolved by inspecting the current chain for
the output ID and by the existing durable publication outcomes.

Write intent is recorded atomically with ordinary root acquisition. Maintenance
publication yields to existing write intents and active staging work. A ready
job briefly gates new writer admission so continuous writes cannot starve its
metadata installation; that renewed gate expires within five seconds after a
worker stops. The two-minute copy/index ownership lease remains separate.
Hard-bound and admission-gate failures return SQLSTATE 40001 before publication;
clients retry whole statements/input with backoff. SQL/input is never replayed by
the driver. Read acquisition is unaffected. Failed workers release ownership for
another connection and record non-cancellation errors with the durable job.

The background worker independently reclaims abandoned publication manifests,
avoiding work-slot exhaustion when maintenance loses root CAS races. Compaction
preserves exact values and row IDs and never mutates old segments. Its total copy
work can still be large and its indexes can take time to build; job concurrency,
memory pressure and pinned snapshots can limit throughput. No performance or
latency guarantee is made. DROP removes a descriptor; TRUNCATE/replace clear its
segment chain and allocate fresh storage. Metadata-only writes reuse segments.

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
unexpired staging manifest, plus every durable compacting work record even after
its owner lease expires. Only an unreferenced segment can transition
to deleting. New references cannot be published to deleting data. The reaper then
drops only that generation's indexes, row keys and directory, then removes its
data record.
A physical segment can outlive its creator manifest. Unpinned retired manifests
conservatively retain data until their own reclamation; pins keep entire
manifests reachable. Active writers protect inherited and newly allocated data.
Aborted/expired SQL writers cannot renew or mutate those generations. Compactor
ownership can be reassigned only through the job lease, fencing the old token.

Both phases are resumable and idempotent, with a five-second budget per
acquisition and one manifest/data cleanup candidate per attempt. Failed cleanup
retains its record for retry. Legacy physical rekey does not operate on this format. Temporary Close/sweep
publishes descriptor removal; expired-session sweep also checks liveness
atomically at publication. SweepReleased invokes reference-aware reclamation.

Fixed limits: eight segments per table, 16 registered catalog manifests, 128 active pins, 64 MiB conservative
staged command-data accounting and 100,000 row HSET operations per write, and
15 minutes per SQL statement/ingestion including streamed consumption. Changed rows, tombstones
consume the SQL/ingest write budget. Background copy budgets reset per batch. Limits fail without
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
