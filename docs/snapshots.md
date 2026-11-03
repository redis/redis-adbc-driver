# Statement snapshots for managed tables

Statement snapshots are **always enabled for managed tables**, including temporary
managed tables and imported copies of application data. No option is required.
Ordinary Open initializes an empty isolated database. Explicit HASH import and
foreign-index import may initialize alongside application-owned data and indexes,
but reject existing legacy driver metadata without modification. No upgrade or
migration path is included. Storage protocol version 14 rejects prior draft versions
1–13. It requires an indexed tombstone marker for visibility-safe server reduction.
Metadata manifests share immutable buckets and native values through a fixed
256-slot map. Buckets have exact ordered field directories; point metadata reads
fetch individual fields with count/reference proofs. Writes copy touched buckets/values; reference-counted retirement
preserves aliases after their original manifest is removed. Committed map,
bucket and object checks reject incomplete metadata. Reclamation drains bounded
batches without persisting Redis SCAN cursors across restarts or slot moves.
This is the driver storage format, separate from Redis versions and RESP3.

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
  Adopted and foreign-index managed copies expose their original application
  key through the system column `__key`, excluded from `SELECT *` and public
  table schemas. Predicates read copied provenance, never the live application
  HASH or a guessed physical row ID. UPDATE/DELETE, compaction, ALTER/rekey and
  shared-generation partitions preserve it. Rows added only through SQL have
  NULL `__key`. Explicit refresh replaces the copy's rows and inferred schema,
  including private SQL changes. See the [source-key checkpoint](sdlc/changes/snapshot-parity/source-key-validation.md).
- Imported `__key` is read-only provenance. SQL/MERGE/Arrow inserts create private
  rows with NULL provenance. Qualified joins and joined UPDATE/DELETE/MERGE can
  read it, including explicit derived/CTE projections; default stars and NATURAL
  join columns still use public schemas. Arrow replace uses the supplied public
  schema/data while retaining the import origin and nullable provenance, so a
  subsequent explicit refresh remains available. Failed replacement publishes
  neither a partial schema nor rows; pinned old streams keep their generation.
- Import source indexes serve capture only. Managed copies build native private
  indexes after decoding; original text encodings and source TAG/NUMERIC choices
  do not limit private index eligibility. AdoptHashes `IndexColumns` selects
  public columns independently of a selected source index (nil: eligible defaults;
  an empty Go slice: none). Internal row/provenance attributes remain. The report's
  `SourceIndex` / `source_index` identifies capture, while SQL discovers its private
  index through information_schema.tables. Requested incompatible/missing source
  indexes block Apply with a complete report and preserve the previous copy.
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
- InspectHashes is available alongside snapshot catalogs. SweepReleased previews
  registered zero-reference row generations without connection-side reclamation.
  Preview reports currently eligible HASHes; `metadata_pending` / `more` indicate
  that retirement can expose additional data. Apply drains at most 256 maintenance
  rounds with bounded directory pages, returns confirmed row deletion counts and
  retired-manifest counts, and reports errors with partial progress. Repeat Apply
  when `more` is true. Active reader/writer references and unregistered application
  objects remain protected; no legacy prefix scan runs in snapshot mode. Reports
  set `managed_snapshots=true`; `released` counts eligible data generations observed
  in that pass, rather than the historical legacy release registry. Concurrent
  background GC can change eligibility between preview and Apply. See the
  [sweep checkpoint](sdlc/changes/snapshot-parity/sweep-validation.md).
- Live partition descriptors (version 3) retain one producer generation for the
  entire set. Independent workers read that generation across concurrent DML,
  schema changes, TRUNCATE/DROP, producer close and compaction. Each active reader
  owns a child pin. `adbc.redis.partition_result_ttl` sets descriptor admission
  lifetime (default five minutes, maximum 24 hours); expired/released descriptors
  fail explicitly. Get statement option `adbc.redis.partition_snapshot_id` after
  production and use `DROP PARTITION SNAPSHOT '<id>'` to release the group early.
  Release prevents new readers while active child pins can finish. Closing the
  producer or freeing descriptor bytes does not revoke workers. Managed catalogs
  reject legacy live descriptors without a shared generation; regenerate them.
- Retained production captures managed sources from one pinned generation and
  publishes independent Arrow results. Existing partition shape, quota and
  deployment restrictions still apply. Independent retained reader-only
  connections remain available.
- `adbc.redis.rename_rekey=true` creates a new private physical identity during
  RENAME and publishes it atomically. Pinned readers keep the old identity.
  Rewrites remain subject to statement staging budgets.
- Supports primary proxy endpoints and Cluster API connections, with TLS and
  RESP3. Every primary must use `maxmemory-policy noeviction`; Active-Active is
  unsupported. Open checks each primary and installs fences on direct node
  connections, including nodes discovered after topology changes.
- No live migration or downgrade is included. See the
  [Cluster API and recovery evidence](sdlc/changes/snapshot-cluster/validation.md)
  for the tested Software topology and recovery boundaries.
- Protocol 13 separates row data from the metadata ACL namespace. Row prefixes
  are `<escaped-schema>:{meta}:snapshot:rows:<uuid>:`; schema `adbc` uses `%61dbc`
  so `%R~adbc:*` cannot read its rows. The old protocol-12 cross-schema data
  exposure is covered by the [schema-isolation checkpoint](sdlc/changes/snapshot-parity/schema-namespace-validation.md).
  Complete restricted-driver command/pin recipes still require validation.

Distributed Search schema commands run at the top level, bracketed by server-side
writer admission and validation. Running FT.CREATE inside Lua produces a local
index outside Software's distributed registry. A late global CREATE after lease
loss cannot publish; maintenance removes its private orphan index. Row and metadata
mutations remain inside atomic Lua fences. Private staging reads resolve
immutable inputs and their unindexed outputs through authoritative row directories.
After all changes, a single fresh index scans each final immutable output before
publication; document counts alone do not prove that incremental updates have
settled. CREATE/CTAS, ordinary DML, ALTER rewrites and TRUNCATE build no temporary
working index. UPDATE/DELETE statements that write no row images or tombstones
return without publishing a new empty segment or building an index.

Metadata/control records use `adbc:{meta}:`; row HASHes use their escaped
schema namespace and retain `{meta}`. The graph records each data generation's
exact physical prefix. Writer admission and descriptor registration reject
another prefix with the same UUID; reclamation uses the registered prefix after
schema removal. All snapshot rows, metadata and control records use `{meta}`. This deliberately
places data in one hash slot; this version does not distribute a catalog's rows
across shards. Use a deployment that honors Redis hash tags and reads from the
primary. Publication inherits Redis's configured persistence/replication
durability. Referenced row sources reject missing, failed or rebuilding published indexes.

Only protocol-compliant clients may write snapshot data. Use a dedicated
catalog; this release does not upgrade existing driver storage. An old executable or raw Redis client
cannot be protected by a new driver's option check. Do not mutate, expire or
evict published keys or indexes outside the driver.

## Costs, limits and recovery

Each write copies at most 256 metadata bucket references. A touched bucket is
constructed in renewable, checkpointed batches of at most 64 references, targeting
1 MiB of copied field/owner bytes; one oversized field may exceed that target,
subject to server key limits. Checkpoints use logical keys, not Redis SCAN
cursors. Partial targets and their source buckets retain explicit references;
abort/expiry retires both, and incomplete construction cannot publish. Native
values remain private only when actually mutated. Touched buckets share
native metadata values until a logical key is changed; unchanged table descriptors
and row/index storage remain shared. Final index sealing visits dirty tables.
Snapshot UUID ownership replaces the legacy catalog-wide prefix/index registries.
Each immutable native table descriptor retains its row segments independently of
bucket aliases. Private copies inherit these references before replacing shared
metadata; descriptor SET updates them inside the writer fence. Publication releases
only staging-owned output references, without reading unrelated table descriptors.
Durable compaction jobs retain their own source/output references. A zero-reference
queue admits row reclamation in bounded batches without scanning all catalog data.
Each row cleanup batch atomically deletes at most 256 registered row keys and
removes their directory entries after rechecking the deleting owner and zero
references. Interruption or a lost reply retains this progress for the next
collector; acknowledged deletion counts exclude uncertain replies. Cleanup
preflights DEL/ZREM permissions before either mutation. Stale collectors skip
already consumed entries rather than deleting unregistered objects at old keys.
Publication tracks metadata versions read by planning/execution, including reads
before a workspace is reserved. If another writer publishes unrelated changes,
the driver seals its outputs, checks those original dependencies against a pinned
latest generation and merges only changed logical keys. Bucket collisions preserve
both updates. Changed read/target metadata and shared catalog collections still
cause serialization conflicts; sustained publication contention can also exhaust
the bounded metadata merge attempts. SQL, Arrow input, defaults and volatile
expressions are never re-executed. Both the original and latest pins must remain
valid through publication. INSERT and append ingestion write new
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
may materialize. Segmented reads can use Search candidates, resolving newer exact-directory
membership before fetching surviving rows. Numeric keyset pages avoid deep
OFFSET/cursor truncation. Below 2^53 row IDs, bounded visibility masks restore
server aggregation, DISTINCT, grouping sets and globally merged ordered scans;
OFFSET/LIMIT apply once after exact merging, including rounded numeric ties.
AVG combines SUM and non-NULL COUNT. Parallel exact scalar/top-K and tuple paths
use the same visibility rules. Empty private tails can use immutable source
indexes for selective DML discovery; populated private indexes remain on the
HASH/directory path. Prefix expansion completeness is proved on every segment.
Eligible wide, simple Arrow projections fetch positional HASH fields only after
resolving the newest physical versions. They validate row identity and deletion
markers before projection and reject missing referenced HASHes. Ascending row-ID
merges preserve order without a separate ordered reduction. Live partition
directory bounds are enforced exactly; the caller's predicate remains active.
Residual predicates, inexact boundaries, pushed LIMIT and descending order retain
their existing fallbacks.
Large masks (over 4,096 overlapping IDs or 256 coalesced ranges), unsettled private
indexes and inexact ID ranges use authoritative bounded fallbacks. Performance
and remaining parity work are tracked in the [parity record](sdlc/changes/snapshot-parity/plan.md).

Background compaction is automatic while managed connections are open. Publication
updates a durable queue only for changed table descriptors; idle discovery reads
no table descriptors. Candidates retain their original queue age across updates,
carry table/lineage identities and generation prefixes, and are consumed atomically
when a durable job starts. Stale cleanup cannot remove a newer candidate. A worker
may claim an older immutable prefix across newer deltas; installation preserves
those tails. Failed candidate checks are deferred so other due work can progress.
Discovery considers at most 16 due candidates per pass. It starts at four segments and copies an immutable prefix in resumable batches of at most
64 merged row IDs, targeting 1 MiB of write data per batch. One oversized row may
exceed that target under the normal 64 MiB hard staging limit. Source fetches
remain bounded by row count, rather than the soft write-byte target. The complete
job is not subject to the SQL statement's 100,000-row or 15-minute total limit.
Normal writes allocate deltas and never perform compaction row copies.

A table still has a hard eight-segment bound. Writable managed connections wait
for their background worker to make room before reserving a workspace, planning
SQL or consuming Arrow input. Waiting releases the write-intent pin so compaction
can publish, then admits a fresh generation. The short compaction publication
gate also waits before execution, with bounded polling and the statement's
cancellation/deadline. Existing immutable-index proofs are shared with the private
workspace. No SQL or Arrow input is replayed. Reads remain available.

Direct/internal stores without a background worker retain explicit SQLSTATE
`40001` backpressure at the hard segment limit. Concurrent publication conflicts
can still return `40001`; independent metadata publication is ongoing work.
The admission gate expires within five seconds if the worker stops renewing it;
compaction ownership uses a separate two-minute lease. Existing write intent is
registered with root acquisition, so maintenance cannot invalidate an admitted
writer in the gap before workspace reservation.

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
Manifest copying, per-segment readiness checks and Search initial
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
| 256 row images / 1 MiB target per atomic write batch | INSERT/UPDATE/DELETE, Arrow ingestion and ALTER/rekey stage complete images or tombstones plus exact directory registration; a single wide row may exceed the target up to 64 MiB of command arguments |
| 4,096 converted Arrow rows per slice | Bounds the driver's additional row conversion; the caller's original Arrow record remains retained while consumed |
| 64 MiB staged command data per unbatched write | Conservative count for metadata and remaining unbatched utility paths; managed row batches and compaction enforce separate per-batch limits; not a Redis memory estimate |
| 100,000 unbatched row HSET operations per write | Remains on unbatched helper/utility mutations; bounded SQL/Arrow row images and background copy jobs are not subject to this cumulative limit. Imports retain their independent capture quotas |
| 15 minutes per statement/ingest | Includes complete streamed consumption |
| Two-minute lease, renewed every 30 seconds | Ownership loss fails the operation; expired pins cannot be resurrected |

Row-image batches retain one bounded outcome in their private work record. A lost
reply is resolved with an independent read; the driver never resends an uncertain
batch or rereads SQL/Arrow input. Rows and directory entries precede the outcome,
and the final catalog publication still follows complete input, constraints and
index validation. Failed/unknown batches cannot publish. There is no public
resume token or process-crash resume for unread Arrow input. SQL planning can
still materialize complete result rows, and whole-statement timeouts remain.
Replacement batches can clear only registered HASHes in the writer's private
generation; published/shared sources and unregistered objects cannot be replaced.
Complete replacement removes omitted NULL fields, preserves imported provenance
and tracks tombstones before final index publication.

Redis also needs memory for retained generations, indexes and server overhead.
Concurrent publication returns SQLSTATE `40001` to the losing writer. Retry the
whole statement with fresh input; SQL, volatile expressions and Arrow streams
are never automatically replayed. Writes to unrelated tables also conflict.

Lost publication replies are resolved from stored operation outcomes. An
indeterminate outcome returns an error containing the operation ID and advising
against blind retry. Outcomes remain until their generations are reclaimed;
they are not a permanent audit log.

Acquisition, renewal and cleanup claims are atomic. Private row/metadata mutations check the writer fence atomically on the server.
Global Search schema mutations use pre/post admission; a late unreferenced index
cannot publish and is reclaimed. Cancellation, exhaustion, release and handle closure release reader
ownership. Later operations reclaim unpinned retired generations and abandoned
expired staging generations. Cleanup is idempotent and bounded per acquisition;
failed cleanup retains resources for a later attempt. Atomic reference checks
prevent deleting shared table generations while a manifest can still use them.
Process-crash recovery waits for the abandoned writer's lease to expire.

In addition to ordinary Search/HASH/metadata commands, the mode requires `EVAL`,
`TIME`, `DBSIZE`, `DUMP`, `RESTORE`, `SCAN`, `ZRANGE`, `ZLEXCOUNT`, `ZCARD`, `ZSCORE` and
`ZRANGEBYLEX` and `INFO`, plus namespace access. Lua directory registration also
requires `ZADD` and `ZREM`.
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

Statement acquisition checks the committed metadata directory. Each referenced
table validates its immutable segment indexes and exact directory counts once
per statement, including concurrent readers within the statement. An unavailable
index on an unrelated table does not prevent reading a healthy table. Referenced
missing or rebuilding indexes fail explicitly; direct row-ID reads also reject a
directory entry whose authoritative HASH is missing. Private outputs are checked
by writer fences and the final publication seal.
