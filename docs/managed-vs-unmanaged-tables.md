# Managed tables and unmanaged Redis data

**Managed tables are the default for SQL and Arrow workflows.** They give the
Redis ADBC driver control over schema, storage and
publication of changes. Unmanaged data remains under an application's control,
usually as Redis HASHes and application Search indexes. The choice is primarily
about ownership, consistency and freshness. Both approaches have benefits and
costs; choose the one that fits the workload, or use both.

An **unmanaged table** is a SQL registration over application-owned Redis data.
Explicit [unmanaged mode](unmanaged-tables.md) reads and writes the original
HASHes without copying them or providing managed row snapshots. HASH adoption
and foreign-index access default to **managed copies**; select unmanaged mode
when source freshness and application ownership are the desired contract.

## Choose by use case

| Approach | Good fit | Benefits | Tradeoffs |
|---|---|---|---|
| **Managed tables (default)** | SQL/Arrow pipelines, typed datasets and consistent analytical reads where the driver can own storage | Typed schema, statement snapshots, atomic supported writes and automatic version maintenance | Private storage/index costs, retained versions, driver ownership rules and fixed limits; no multi-statement write transactions |
| **Unmanaged source tables** | Operational workloads centered on existing keys, TTLs and source updates | Live SQL reads and source writes; application controls indexes and lifecycle; no data copy | Requires a source Search index; application owns coordination; reads have no row snapshot and writes can partially succeed |
| **Both, connected by managed imports** | Reporting over application data with an explicit freshness budget | Source ownership stays with the application; the private SQL copy has managed read guarantees | Capture time, duplicate storage and refresh work; source capture is not atomic with application writers; refresh replaces private changes |

## At a glance

| Aspect | Managed table | Unmanaged application data |
|---|---|---|
| Owner | Driver owns private rows, metadata, directories and indexes | Application owns keys, field encodings, indexes and lifecycle |
| Creation | SQL, Arrow ingestion, or import into a managed copy | Application writes Redis data directly |
| Schema | Driver maintains SQL types and supported defaults and constraints | Application defines and enforces its own data contract |
| Access | SQL and Arrow through the driver | Redis commands and application Search indexes; explicit unmanaged registration for SQL reads and writes |
| Read consistency | One committed version per statement; optional shared read snapshot across sequential reads | Depends on application access; incremental scans and live HASH reads do not freeze a collection |
| Write publication | Supported SQL/Arrow changes publish atomically | Depends on the application's commands and coordination |
| Concurrent changes | Open readers retain their original managed version | Direct reads can observe intervening application writes |
| Freshness after import | Captured values remain until explicitly refreshed or changed through the driver | Application changes affect the source directly |
| Expiry | Driver-owned storage must not be directly expired or evicted | Application can use TTLs according to its own design |
| Maintenance | Driver manages private indexes, compaction and reference-safe reclamation | Application manages its keys and indexes |
| Storage cost | Private data/indexes plus metadata, retained versions and staging | Catalog registration only; source data/index storage costs still apply |

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
- **No mandatory analytical duplicate.** Unmanaged SQL registrations avoid
  private data copies and version-retention costs.
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
- **Weaker SQL write guarantees.** Unmanaged SQL writes change source HASHes
  directly and can partially succeed. INSERT requires an explicit `__key`.
  ALTER, TRUNCATE, Arrow ingestion and snapshot-only APIs are unsupported.

## How imports connect the two

By default, `AdoptHashes` creates a managed copy. Repeating Apply for the same source and
destination explicitly refreshes that copy. `CopyHashes` copies source data into
managed storage using its supported ingestion modes. Enabling
`adbc.redis.foreign_indexes=true` imports missing copies into the read-only
`redis_foreign` schema; existing copies need an explicit refresh to recapture
source changes. Select `HashAdoptOptions.Mode="unmanaged"` (CLI
`-mode unmanaged`) or `adbc.redis.foreign_indexes_mode=unmanaged` for live source
registrations. These need no refresh for source values; explicit refresh
updates their registered schema. The driver never silently changes a table's
ownership mode.

Capture is not atomic with application writers. If a consistent point-in-time
source image is required, pause source writes during capture. After publication,
the managed copy has normal statement snapshot guarantees.

Managed adopted and foreign-index copies retain the original source key in the hidden,
read-only `__key` provenance column. It is excluded from `SELECT *`; SQL-added
rows in writable managed copies have NULL provenance. SQL writes affect the
private copy, not the source. Source TTL expiry does not expire the managed copy.

For example, if an application changes `user:42` after importing `users`, direct
Redis reads can see the new source value while SQL still sees the captured value.
An explicit refresh publishes a new managed version. An already open SQL reader
continues using its earlier version until released or its snapshot becomes invalid.

## Choosing an approach

Start with **managed tables**, the default, when SQL/Arrow consumers need typed data, atomic publication
and consistent analytical reads, and the driver can own the storage lifecycle.

Keep **application data unmanaged** when direct Redis access, application-defined
keys, TTLs and source freshness are central. Register unmanaged SQL tables for
live source access, or import managed copies when reporting needs justify the
additional storage and refresh process.

A common arrangement uses both: application-owned HASHes for operational data
and explicitly refreshed managed copies for SQL analytics. Choose refresh
frequency based on acceptable staleness and import cost; benchmark the actual
workload before making performance assumptions.

## Further reading

- [Statement snapshots, imports and storage lifecycle](snapshots.md)
- [Unmanaged registration, source writes and limits](unmanaged-tables.md)
- [Connection-scoped shared read snapshots](shared-read-snapshots.md)
- [HASH import and refresh reference](driver-reference.md#import-or-refresh-a-managed-copy)
- [SQL authorization and ownership policies](sql-rbac.md)
