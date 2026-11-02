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

Writes copy the metadata manifest and reuse unchanged immutable table generations.
DML targets are copied independently; only owned table indexes are rebuilt.
Row-level delta storage remains outside this refinement.
Target detachment happens before the existing DML planner captures row keys;
source relations retain shared descriptors, so no late target-key translation is
needed. Row-level deltas and compaction are deferred.

## Storage and publication

All control records, metadata and row keys share `{meta}`. Index names also
include this tag. Cluster API connections, non-primary endpoints, observed
Active-Active CRDT configurations and eviction policies other than noeviction
are rejected. A single endpoint with working hash-tag routing is required. The
verified topology is one Redis Software shard with Search and active 4x QPF.

The control hash `adbc:{meta}:snapshot:catalog` contains:

| Field | Contents |
| --- | --- |
| `format` | Protocol version `2`; prior draft version 1 is rejected |
| `root` | Unique committed catalog generation ID |
| `w:<generation>` | Staging owner token, base root and lease expiry; committed outcome; or deletion claim |
| `p:<token>` | Reader/writer base root and lease expiration |
| `r:<manifest>` | JSON array of referenced physical table-generation IDs |
| `d:<table-generation>` | Original staging owner and live/deleting state; lifetime independent of creator manifest |

Legacy-shaped metadata keys are scoped to
`adbc:{meta}:snapshot:workspace:<generation>:` at the client command boundary.
Only key positions are rewritten; values, SQL literals, set members and script
bodies are preserved. Rows live under
`adbc:{meta}:snapshot:rows:<table-generation>:`. Index names identify the same
physical table generation. Multiple catalog manifests may reference that same
immutable prefix/index. IDs and physical names are never reused.

A staging manifest atomically inherits its base manifest's table references
when reserved. Every new physical table generation is registered and added to its
staging manifest's references before allocation. Metadata is copied with
DUMP/RESTORE; unchanged table descriptors, rows and indexes are shared.
INSERT/UPDATE/DELETE/MERGE targets are detached before planning captures row keys;
append/create_append ingestion detaches an existing target once for the complete
input. Only target HASH fields are copied, without interpreting their values.
Read-only source tables remain shared. DROP removes a descriptor and TRUNCATE or
replace ingestion allocates fresh storage without copying discarded rows;
physical cleanup is deferred to reference-aware reclamation. Metadata-only writes
reuse table data. Row identities, counters,
constraints, physical fields, missing defaults and exactness metadata survive.
All published planning metadata is immutable; legacy background tag checks are
disabled in this format.

Each private mutation runs inside a Lua writer fence, declaring its control and
data keys explicitly. The fence checks the generation's owner, staging state and
server-time lease in the same operation as the mutation. Physical mutations also
check every addressed table generation's original owner and live state. Writes
to inherited shared data are rejected both by the Go scope and the server fence.
A resumed expired writer cannot recreate reclaimed rows, metadata or indexes.

Private copies receive an index built by initial scan after their HASHes are
copied, and readiness is validated before DML reads those copies. After execution,
fresh indexes initially scan the final immutable row images of owned tables.
Shared indexes are neither rebuilt nor row-scanned by writes. Publication waits
for complete initial scanning and compares each owned table's authoritative row
count with its index document count; indexing failures prevent publication. Incremental
update counts alone are not considered a readiness fence. The
count is stored in the immutable descriptor. Acquisition verifies descriptors
and index identity, row count, indexing state and failure count, failing closed
on missing/rebuilding indexes. Cursor-listed rows that disappear fail reads;
absent direct-key lookups still legitimately return no row. RESP3 partial-result
warnings fail snapshot searches and close affected cursors.

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
drops only that generation's indexes and row keys and removes its data record.
A physical table can outlive its creator manifest. Unpinned retired manifests
conservatively retain data until their own reclamation; pins keep entire
manifests reachable. Active writers protect inherited and newly allocated data.
Aborted/expired writers cannot renew or mutate those generations.

Both phases are resumable and idempotent, with a five-second budget per
acquisition and one manifest/data cleanup candidate per attempt. Failed cleanup
retains its record for retry. Legacy sweep/rekey/temporary cleanup does not
operate on this storage format.

Fixed limits: 16 registered catalog manifests, 128 active pins, 64 MiB conservative
staged command-data accounting and 100,000 row HSET operations per write, and
15 minutes per SQL statement/ingestion including streamed consumption. Copying target-table rows
and subsequent updates both consume the write budget. Limits fail without
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
