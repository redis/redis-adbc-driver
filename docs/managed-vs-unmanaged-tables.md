# Managed tables and unmanaged Redis data

Managed tables give the Redis ADBC driver control over schema, storage and
publication of changes. Unmanaged data remains under an application's control,
usually as Redis HASHes and application Search indexes. The choice is primarily
about ownership, consistency and freshness.

In this guide, **unmanaged table** means an application-owned collection of Redis
data. It is not a second SQL table mode with the same guarantees. In the current
driver, HASH adoption and foreign-index access import **managed copies**; they do
not expose a continuously updated SQL view over the original keys. Historical
in-place adoption documentation does not describe the current snapshot format.

## At a glance

| Aspect | Managed table | Unmanaged application data |
|---|---|---|
| Owner | Driver owns private rows, metadata, directories and indexes | Application owns keys, field encodings, indexes and lifecycle |
| Creation | SQL, Arrow ingestion, or import into a managed copy | Application writes Redis data directly |
| Schema | Driver maintains SQL types and supported defaults and constraints | Application defines and enforces its own data contract |
| Access | SQL and Arrow through the driver | Redis commands and application Search indexes; import for managed SQL access |
| Read consistency | One committed version per statement; optional shared read snapshot across sequential reads | Depends on application access; incremental scans and live HASH reads do not freeze a collection |
| Write publication | Supported SQL/Arrow changes publish atomically | Depends on the application's commands and coordination |
| Concurrent changes | Open readers retain their original managed version | Direct reads can observe intervening application writes |
| Freshness after import | Captured values remain until explicitly refreshed or changed through the driver | Application changes affect the source directly |
| Expiry | Driver-owned storage must not be directly expired or evicted | Application can use TTLs according to its own design |
| Maintenance | Driver manages private indexes, compaction and reference-safe reclamation | Application manages its keys and indexes |
| Storage cost | Private data/indexes plus metadata, retained versions and staging | No driver-managed copy until imported; application storage/index costs still apply |

## Managed tables: benefits

- **Consistent analytical reads.** Joins, CTEs, subqueries, cursor pages and
  streamed Arrow batches use one committed catalog generation. Another connection
  can update, truncate or drop a table while an existing reader retains its view.
- **Atomic changes.** Supported DML, DDL and Arrow ingestion stage private work
  before publication. A late ingestion failure does not expose earlier batches
  as a partially updated table.
- **Typed SQL access.** The driver maintains table definitions and applies its
  supported conversion, default and constraint rules. SQL and Arrow consumers
  share that managed representation.
- **Stable schema and data together.** Existing readers retain the schema and
  values they started with across supported schema changes.
- **Consistent exports across tables.** An explicitly acquired connection read
  snapshot keeps discovery and sequential supported SELECTs on one generation.
- **Automatic version maintenance.** The driver compacts row-version segments
  and reclaims retired storage after reader and writer references are released.

## Managed tables: costs and limitations

- **Additional storage and work.** Imported data is copied, and private Search
  indexes are built. Staged writes and retained old versions consume capacity;
  ordinary DML appends changed versions rather than copying the entire table.
- **Ownership requirements.** Applications must avoid directly changing,
  deleting, expiring or evicting driver-owned keys and indexes. Use the documented
  ACL and noeviction policies.
- **Retention can delay reclamation.** Long-lived readers keep older data alive.
  Snapshot, generation, statement and capacity limits can reject or backpressure
  work. Close readers promptly.
- **Imports need refresh management.** Application source changes are not
  automatically synchronized. Refreshing an adopted copy replaces its rows and
  inferred schema, including private SQL changes made since capture.
- **Statement atomicity has a boundary.** Multi-statement write transactions are
  unsupported. A shared read snapshot is read-only, connection-local and has a
  fixed maximum lifetime. Neither API is a backup or arbitrary time-travel API.
- **Distribution has a boundary.** The managed catalog and its versioned storage
  occupy one Redis hash slot. Snapshots do not coordinate independent catalogs
  or independently distributed application shards.

## Unmanaged data: benefits

- **Application control.** Existing key names, encodings, indexes, TTLs and Redis
  access patterns remain under application ownership.
- **Immediate source updates.** Application clients read and write the source
  without waiting for a driver import or refresh. Search visibility follows the
  source index's own behavior.
- **No mandatory analytical duplicate.** Keeping data unmanaged avoids the
  private copy and version-retention costs until managed SQL access is needed.
- **Independent application lifecycle.** An import leaves the original HASHes
  and application indexes unchanged; the application can continue using them.

## Unmanaged data: costs and limitations

- **No driver snapshot over live source writes.** A scan and successive HASH
  reads may capture different moments. Redis persistence snapshots and atomic
  command execution do not by themselves create a reusable driver query view.
- **Application-owned schema quality.** Missing fields, inconsistent encodings
  or incompatible values must be handled by the application and import policy.
- **Application-owned coordination.** Collection-wide atomicity, schema changes
  and safe lifecycle operations require an application design outside the
  managed-table protocol.
- **An import boundary for SQL.** Current adoption and foreign-index imports
  create private tables rather than providing live SQL access to source keys.
  That introduces capture time, storage cost and refresh latency.

## How imports connect the two

`AdoptHashes` creates a managed copy. Repeating Apply for the same source and
destination explicitly refreshes that copy. `CopyHashes` copies source data into
managed storage using its supported ingestion modes. Enabling
`adbc.redis.foreign_indexes=true` imports missing copies into the read-only
`redis_foreign` schema; existing copies need an explicit refresh to recapture
source changes.

Capture is not atomic with application writers. If a consistent point-in-time
source image is required, pause source writes during capture. After publication,
the managed copy has normal statement snapshot guarantees.

Adopted and foreign-index copies retain the original source key in the hidden,
read-only `__key` provenance column. It is excluded from `SELECT *`; SQL-added
rows in writable managed copies have NULL provenance. SQL writes affect the
private copy, not the source. Source TTL expiry does not expire the managed copy.

For example, if an application changes `user:42` after importing `users`, direct
Redis reads can see the new source value while SQL still sees the captured value.
An explicit refresh publishes a new managed version. An already open SQL reader
continues using its earlier version until released or its snapshot becomes invalid.

## Choosing an approach

Use **managed tables** when SQL/Arrow consumers need typed data, atomic publication
and consistent analytical reads, and the driver can own the storage lifecycle.

Keep **application data unmanaged** when direct Redis access, application-defined
keys, TTLs and source freshness are central. Import managed copies when reporting
needs justify the additional storage and refresh process.

A common arrangement uses both: application-owned HASHes for operational data
and explicitly refreshed managed copies for SQL analytics. Choose refresh
frequency based on acceptable staleness and import cost; benchmark the actual
workload before making performance assumptions.

## Further reading

- [Statement snapshots, imports and storage lifecycle](snapshots.md)
- [Connection-scoped shared read snapshots](shared-read-snapshots.md)
- [HASH import and refresh reference](driver-reference.md#import-or-refresh-a-managed-copy)
- [SQL authorization and ownership policies](sql-rbac.md)
