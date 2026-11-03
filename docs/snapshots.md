# Statement snapshots for managed tables

Statement snapshots are **always enabled for managed tables**, including temporary
managed tables and imported copies of application data. No option is required.
Ordinary Open initializes an empty isolated database. Explicit HASH import and
foreign-index import may initialize alongside application-owned data and indexes,
but reject existing legacy driver metadata without modification. No upgrade or
migration path is included. Protocol version 5 also rejects prior draft versions
1–4, whose writers do not retain the new temporary/imported data references.

```python
import adbc_driver_manager.dbapi

with adbc_driver_manager.dbapi.connect(
    driver="/path/to/libadbc_driver_redis.dylib",
    db_kwargs={
        "uri": "redis://localhost:6379/0",
    },
    autocommit=True,
) as conn:
    with conn.cursor() as cur:
        cur.execute("CREATE TABLE measurements(id INTEGER, value BIGINT)")
        cur.execute("INSERT INTO measurements VALUES (1, 9007199254740993)")
        cur.execute("SELECT value FROM measurements")
        assert cur.fetchall() == [(9007199254740993,)]
```

Use the installed library's `.so`, `.dylib` or `.dll` path for your platform.
The database and connection report `adbc.redis.statement_snapshots` as `true`;
setting it to `false` is rejected. Passing `true` explicitly is accepted but
unnecessary.

## Guarantee

Each SELECT acquires one committed catalog generation before planning. Joins,
views, CTEs, subqueries, direct lookups, pushed aggregates, cursor pages and
subsequent exact-value HASH reads all use that generation. A streaming Arrow
reader retains it while other connections publish changes, truncate or drop
its tables. Release streams promptly when their results are no longer needed.

Supported writes build private generations and publish with one atomic pointer
change. Readers see the old or new generation. An error in a later Arrow ingest
batch leaves the published table unchanged. DML RETURNING is exposed after
publication succeeds.

Scripts and bound parameter rows acquire a fresh snapshot for each executed
statement. Statement snapshots do not enable multi-statement transactions: ADBC
autocommit=false and Commit/Rollback remain unsupported; SQL transaction controls
retain their existing compatibility behavior, including no-op ROLLBACK.

## Operations and deployment

- Supported: permanent managed tables, schemas and views; SELECT, SQL DML,
  CREATE TABLE/CTAS, CREATE/DROP VIEW, DROP TABLE, TRUNCATE, COMMENT and schema
  creation/removal. All Arrow ingest modes publish their complete input once.
- Table schema discovery, parameter/schema planning and SQL catalog reads use
  pinned generations.
- ALTER TABLE uses the existing SQL conversion/constraint rules and stages a
  private replacement segment before atomic publication. Earlier readers keep
  their old schema and values. DDL rewrites remain subject to statement budgets;
  ordinary DML continues to append deltas without copying existing rows.
- Temporary tables/views belong to their connection and shadow unqualified names.
  Their metadata and references survive other connections' publications. Close
  and abandoned-session sweep publish removal, preserving pinned storage.
- AdoptHashes now imports a managed copy rather than adopting mutable HASHes in
  place. Repeating Apply for the same source and destination explicitly refreshes
  that copy. A destination created independently cannot be replaced by adoption.
  CopyHashes supports the existing ingest modes with atomic publication.
- `adbc.redis.foreign_indexes=true` imports missing managed copies into the
  read-only `redis_foreign` schema. Existing copies are reused on later Open.
  Set the connection option `adbc.redis.foreign_indexes_refresh=true`, or call
  the Go `RefreshForeignIndexes` API, to refresh existing copies explicitly.
  Each index refresh publishes separately; explicit refresh also removes copies
  whose source indexes no longer exist. Sources overlapping driver control
  storage are rejected. HASHes and application indexes remain application-owned.
- External capture is a scan of observed data, not an atomic transaction with
  application writers. Pause source writes during refresh if a point-in-time
  source image is required. Once published, the private managed copy has normal
  atomic statement snapshots; later application writes appear only after refresh.
- InspectHashes is available alongside snapshot catalogs. SweepReleased uses
  snapshot reference-aware reclamation in these catalogs; it does not run the
  legacy released-prefix deletion scan or report legacy row-deletion counts.
- Live and retained partition production are available. Each live ReadPartition
  pins its own generation through consumption; a set of deferred live reads does
  not share one snapshot, and layout changes can invalidate a descriptor.
  Retained production captures managed sources from one pinned generation and
  publishes independent Arrow results. Existing partition shape, quota and
  deployment restrictions still apply. Independent retained reader-only
  connections remain available.
- No live migration, downgrade or physical rename/rekey tool is included.
- Requires a primary single endpoint, no Active-Active replication and
  `maxmemory-policy noeviction`. Open verifies INFO and rejects Cluster API
  connections. Runtime evidence covers Redis Software 8.6.2 with Search 8.6.11,
  one shard and active 4x QPF.

Private copies are indexed by a completed initial scan before DML can read them.
After all changes, fresh indexes scan the final immutable HASHes before publication;
document counts alone do not prove that incremental updates have settled.

All snapshot rows, metadata and control records use `{meta}`. This deliberately
places data in one hash slot; this version does not distribute a catalog's rows
across shards. Use a deployment that honors Redis hash tags and reads from the
primary. Publication inherits Redis's configured persistence/replication
durability. Acquisition rejects missing, failed or rebuilding published indexes.

Only protocol-compliant clients may write snapshot data. Use a dedicated
catalog; this release does not upgrade existing driver storage. An old executable or raw Redis client
cannot be protected by a new driver's option check. Do not mutate, expire or
evict published keys or indexes outside the driver.

## Costs, limits and recovery

Each write copies the catalog's **metadata manifest**, but unchanged tables reuse
their existing immutable rows and indexes. INSERT and append ingestion write new
rows into a new segment. UPDATE writes complete replacement images for matched
rows; DELETE writes deletion markers. An ordinary one-row change writes one row
image or tombstone, rather than copying the target table. Existing row images and
segment indexes remain unchanged. New segments get their own indexes.

Each segment has an exact row-ID sorted-set directory. All directory scores are
zero; zero-padded decimal IDs provide exact lexical ordering without double
rounding. Reads merge the bounded segment directories in row-ID order. The newest
version wins, including deletion markers, **before** SQL predicates, grouping,
aggregates, ordering, OFFSET or LIMIT. Direct row-ID lookups search newest to
oldest. Unordered results remain streamed with bounded pages; explicit sorting
may materialize. On segmented tables, Search predicate/aggregate pushdowns and
parallel physical-index shortcuts are disabled until they can honor visibility.
This improves write amplification while increasing read work.

Background compaction is automatic while managed connections are open. It starts
at four segments and copies an immutable prefix in resumable batches of at most
64 merged row IDs, targeting 1 MiB of write data per batch. One oversized row may
exceed that target under the normal 64 MiB hard staging limit. Source fetches
remain bounded by row count, rather than the soft write-byte target. The complete
job is not subject to the SQL statement's 100,000-row or 15-minute total limit.
Normal writes allocate deltas and never perform compaction row copies.

A table still has a hard eight-segment bound. If compaction falls behind, writes
return SQLSTATE `40001` and must be retried with backoff and fresh input. A short
publication admission gate also returns `40001` while ready maintenance waits for
existing writers and installs its manifest. The driver never replays SQL or Arrow
input automatically. Reads remain available. The gate expires within five seconds
if the worker stops renewing it; compaction ownership uses a separate two-minute
lease. Existing write intent is registered with root acquisition, so maintenance
cannot invalidate a writer in the gap before workspace reservation.

One durable job per catalog records its source segments, output identity, last
completed row ID, phase and last recorded error. Source/output references survive
worker lease expiration. Another managed connection resumes after cooperative
release or crash-lease expiration; repeated copies after an uncertain reply are
idempotent. Connection close stops its worker and releases ownership when
reachable. With all connections closed, progress waits for the next open.

When copying finishes, the output is sealed against further row mutations before
its index is built and validated. Publication replaces only the compacted prefix,
preserving all newer deltas and the latest metadata/row sequence. DROP/TRUNCATE or
replacement of that lineage discards the obsolete job. Pinned readers retain old
segments; rows, indexes and directories are reclaimed after the final reference
is gone. Background reclamation prevents abandoned publication work from filling
manifest capacity. Each worker uses a separate connection pool and bounded I/O
contexts. Job concurrency, available memory, retained snapshots and index-building
costs can still limit progress; this is not a throughput or latency guarantee.

DROP removes a descriptor; TRUNCATE and replace ingestion allocate fresh storage
and discard the descriptor's old segment list. Metadata-only changes reuse data.
Manifest copying, per-segment readiness checks, cleanup SCAN and Search initial
index scans retain catalog/database traversal costs. No constant-time or
throughput claim is made. Published directories and HASHes must not be changed
outside the protocol; detected missing directory entries or row images fail the
read instead of exposing an older replacement or a partial successful result.

| Fixed limit | Behavior |
| --- | --- |
| 16 catalog manifests | Includes current, retained, staging and pending manifest cleanup; physical table generations have separate lifetimes and can outlive their creator manifests |
| Eight segments per table | Soft compaction trigger at four; writes return retryable backpressure at eight instead of compacting synchronously |
| One compaction job per catalog | Durable, resumable; holds a work record and protected source/output references |
| 64 merged IDs / 1 MiB target per copy batch | One oversized row may exceed the byte target, within the 64 MiB hard staging limit |
| 128 active pins | Includes readers, writer base snapshots and metadata operations |
| 64 MiB staged command data per write | Conservative count before sending mutations, including metadata and changed SQL/ingest rows; compaction resets this bound per batch; not a physical Redis memory estimate |
| 100,000 row HSET operations per write | Applies to SQL/ingest writes; background jobs copy in separately bounded batches; can be reached below 100,000 stored rows |
| 15 minutes per statement/ingest | Includes complete streamed consumption |
| Two-minute lease, renewed every 30 seconds | Ownership loss fails the operation; expired pins cannot be resurrected |

Redis also needs memory for retained generations, indexes and server overhead.
Concurrent publication returns SQLSTATE `40001` to the losing writer. Retry the
whole statement with fresh input; SQL, volatile expressions and Arrow streams
are never automatically replayed. Writes to unrelated tables also conflict.

Lost publication replies are resolved from stored operation outcomes. An
indeterminate outcome returns an error containing the operation ID and advising
against blind retry. Outcomes remain until their generations are reclaimed;
they are not a permanent audit log.

Acquisition, renewal and cleanup claims are atomic. Private mutations check the
writer fence on the server, so expired writers cannot recreate reclaimed data
or indexes. Cancellation, exhaustion, release and handle closure release reader
ownership. Later operations reclaim unpinned retired generations and abandoned
expired staging generations. Cleanup is idempotent and bounded per acquisition;
failed cleanup retains resources for a later attempt. Atomic reference checks
prevent deleting shared table generations while a manifest can still use them.
Process-crash recovery waits for the abandoned writer's lease to expire.

In addition to ordinary Search/HASH/metadata commands, the mode requires `EVAL`,
`TIME`, `DBSIZE`, `DUMP`, `RESTORE`, `SCAN`, `ZRANGE`, `ZLEXCOUNT`, `ZCARD`, `ZSCORE` and
`INFO`, plus namespace access. Lua membership registration also requires `ZADD`.
Lua fences invoke the underlying write commands, which also need ACL permission.
Read-only SQL clients still need control-hash write permission for reader pins.

## Validation

The snapshot suites **clear the database selected by REDIS_SNAPSHOT_URI, including
its indexes**. Use a disposable database, separately from ordinary suites.

```sh
cd go
REDIS_SNAPSHOT_URI=redis://localhost:16400/0 go test -race -count=1 -run '^TestSnapshot' .
make build
# Python environment: adbc-driver-manager, pyarrow and redis
REDIS_SNAPSHOT_URI=redis://localhost:16400/0 python validation/snapshot_validation.py
```

See [design and validation evidence](sdlc/changes/statement-snapshots/validation.md).
