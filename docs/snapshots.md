# Statement snapshots for managed tables

Statement snapshots are **always enabled for managed tables**. No option is
required. The first connection initializes an **empty, isolated Redis database
with no existing Search indexes**. Existing unversioned catalogs are rejected
without modification. No upgrade or migration path is included. The shared-table design uses protocol
version 2 and also rejects catalogs created by the earlier version 1 draft.

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
- Rejected: ALTER TABLE, temporary tables/views, adopted HASHes,
  foreign/application indexes and legacy HASH inspection/copy/adoption/sweep
  utilities and partition production (both live and retained). Independent
  retained partition reader-only connections remain available. There is no live
  migration or downgrade tool.
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

Only protocol-compliant clients may write snapshot data. Use a fresh isolated
catalog; this release does not upgrade existing storage. An old executable or raw Redis client
cannot be protected by a new driver's option check. Do not mutate, expire or
evict published keys or indexes outside the driver.

## Costs, limits and recovery

Each write copies the catalog's **metadata manifest**, but unchanged tables reuse
their existing immutable rows and indexes. INSERT, UPDATE, DELETE, MERGE and
append ingestion copy only their target table before execution and rebuild only
that table's indexes. The data copied and indexes rebuilt for a one-row UPDATE
are therefore limited to its target table. Read-only source tables in joins, subqueries and INSERT SELECT remain shared.

DROP removes a descriptor; TRUNCATE and replace ingestion allocate empty or
replacement storage without copying the discarded table. Metadata-only changes
reuse table data. Retired manifests and physical table generations are reclaimed
separately: a generation survives while any registered manifest references it,
including pinned readers and active writers. No row-level delta storage or
compaction is implemented yet. Catalog metadata copying, index checks and root
publication still involve catalog-wide work. Prefix-filtered SCAN and Search
initial indexing may also visit database keys outside the target table; this
change does not eliminate those traversal costs. This is not constant-time writing.

| Fixed limit | Behavior |
| --- | --- |
| 16 catalog manifests | Includes current, retained, staging and pending manifest cleanup; physical table generations have separate lifetimes and can outlive their creator manifests |
| 128 active pins | Includes readers, writer base snapshots and metadata operations |
| 64 MiB staged command data per write | Conservative count before sending mutations, including copied metadata/target-table rows and command arguments; not a physical Redis memory estimate |
| 100,000 row HSET operations per write | Includes copied target-table rows and subsequent changes; can be reached below 100,000 stored rows |
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
`TIME`, `DBSIZE`, `DUMP`, `RESTORE`, `SCAN` and `INFO`, plus namespace access.
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
