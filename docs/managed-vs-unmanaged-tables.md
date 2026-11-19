# Table modes: who owns the data and indexes?

The driver has three ownership modes. All three support SQL reads and writes;
what changes is who owns the HASH data, who maintains the Search indexes, and
which consistency guarantees apply.

| Mode | HASH data owner | Search index owner | Direct Redis reads/writes |
|---|---|---|---|
| **Managed** | Driver | Driver | Physical storage is internal; direct writes unsupported |
| **Unmanaged** | Application | Application | Yes, on the original HASHes |
| **Live HASH tables** | Application | Driver | Yes, on the original HASHes |

All HASHes and Search indexes reside on the Redis server. Ownership describes
who creates, changes, and removes them; it does not describe client-side storage.
The driver maintains SQL catalog or registration metadata in every mode. Redis
updates index entries as HASHes change; the index owner controls the definition
and lifecycle.

Unmanaged tables also expose live source data. In this guide, **live HASH tables**
names the mode with application-owned data and driver-owned indexes specifically.

## Choose by use case

| Choose | When it fits | Main tradeoff |
|---|---|---|
| **Managed: driver owns data + indexes** | SQL/Arrow pipelines and analytics that need statement snapshots and atomic supported writes | Write through the driver; importing application data creates a private copy |
| **Unmanaged: application owns data + indexes** | Your application already has a compatible Search index and wants to retain its configuration and lifecycle | The application manages schema compatibility, indexing, and coordination |
| **Live: application owns data + driver owns indexes** | You want to inspect and edit HASHes with Redis clients, use SQL on the same data, and let the driver handle indexes | Per-row writes and live source reads replace managed statement guarantees; initial features are narrower |

Use **live HASH tables** when the requirement is: “I want to access and change my
HASHes directly, and I do not want to manage indexes.” This mode currently
requires a source build. Use managed tables when consistency across a statement
matters more than direct access to application keys.

## At a glance

| Aspect | Managed | Unmanaged | Live HASH tables |
|---|---|---|---|
| SQL data source | Driver-owned rows | Original application HASHes | Original application HASHes |
| Index management | Driver creates and maintains private indexes | Application provides and maintains a compatible index | Driver creates and maintains its own index over the source prefix |
| Creation/registration | SQL, Arrow ingestion, or managed import | Explicit unmanaged adoption or foreign-index registration | Live registration API or `redis-adbc live register` |
| Direct Redis writes | Unsupported for driver-owned storage | Supported; bypass SQL validation | Supported; bypass SQL validation |
| SQL INSERT key | Driver manages physical row identity | Explicit `__key`, or a generated prefixed UUID when omitted/DEFAULT | Explicit `__key`, or a generated prefixed UUID when omitted/DEFAULT |
| Read consistency | One committed managed version per statement | Source values can change during execution | Source values can change during execution |
| SQL write behavior | Supported changes publish atomically | Writes affect source HASHes and can partially succeed | Writes affect source HASHes per row and can partially succeed |
| Source changes | An imported copy needs an explicit refresh | No data refresh; indexed visibility follows the source index | No data refresh; indexed visibility follows the driver-owned index |
| TTLs | Do not directly expire driver-owned rows | Application controls source TTLs | Application controls source TTLs; SQL UPDATE preserves them |
| Arrow ingestion | Supported | Supported within the unmanaged write contract | Append to registered tables |
| Data copy | Managed import creates a copy; SQL-created tables need no external source | No private data copy | No private data copy |
| Storage costs | Rows, indexes, catalog, staging, and retained versions | Source data/indexes plus SQL registration metadata | Source data, driver-owned indexes, and SQL registration metadata |
| Removal | DROP removes the logical table; managed references govern physical reclamation | DROP removes the registration and leaves source data/indexes | Unregister removes the registration and its driver-owned index, leaving source data |

All modes are autocommit-only. Multi-statement transactions and rollback are
unsupported. Index ownership alone does not establish which mode will run a
query faster; benchmark the actual workload and definitions.

## Managed: driver owns the data and indexes

Managed tables are the default for SQL and Arrow workflows. Applications write
through supported driver APIs. The driver controls SQL types, defaults,
constraints, row versions, indexes, and publication.

A statement reads one committed version, including through joins, subqueries,
and streamed batches. Supported SQL and Arrow writes stage their work and publish
atomically. Existing readers retain their view while another connection changes
or drops the table. The driver compacts versions and reclaims retired storage
when references are released.

The cost is private storage and lifecycle control. Long-lived readers retain old
versions, and staging/retention limits can backpressure writes. Direct Redis
reads of physical keys do not resolve row versions or tombstones; direct edits,
expiry, and eviction bypass the storage protocol. Use SQL/ADBC or the supported
[snapshot-aware native reader](native-client.md).

See [managed snapshots](snapshots.md) for the precise guarantees, fixed limits,
ACL/noeviction requirements, and storage compatibility.

## Unmanaged: application owns the data and indexes

Unmanaged registration exposes an application's HASHes through an existing
compatible Search index. The driver does not take ownership of that index or
copy the data. SQL and Redis clients operate on the same source keys.

For an application index named `idx_users` on `app:user:`:

```sh
go/build/redis-adbc adopt -prefix app:user: -table users \
  -index idx_users -mode unmanaged -apply
```

The application remains responsible for the index's schema, settings, memory,
and lifecycle, as well as source field encodings and coordination. Source value
changes need no import/refresh; explicit registration refresh updates the SQL
schema and recorded source definition. SQL INSERT accepts a new explicit `__key`
or generates `<registered-prefix> || GEN_RANDOM_UUID()::TEXT` per row when the
key is omitted or DEFAULT. Arrow ingestion requires explicit keys.

This mode supports direct SQL writes and specific Arrow ingestion operations,
but has no managed row snapshot or atomic multi-row publication. A failure can
leave earlier source writes applied. DROP leaves both source HASHes and the
application index intact.

See [unmanaged registration and writes](unmanaged-tables.md) for compatible
indexes, foreign-index filters/prefixes, permissions, and failure handling.

## Live: application owns the data; driver owns the indexes

Live registration leaves HASHes under application-defined keys while creating a
driver-owned Search index for SQL access. Applications can continue using Redis
clients to inspect data, change fields, delete keys, and set TTLs. The driver
adds no internal row/version fields to those HASHes.

Register an existing collection once:

```sh
go/build/redis-adbc live register -prefix app:users: -table users -apply
```

Enable `adbc.redis.live_tables=true` on the database or connection. The registered
table appears as `redis_live.users`:

```sql
INSERT INTO redis_live.users (name, age)
VALUES ('Grace', 40) RETURNING __key;
```

Omitting `__key`, or supplying DEFAULT, uses a fresh
`<registered-prefix> || GEN_RANDOM_UUID()::TEXT` for each row. Explicit keys remain
supported. SQL UPDATE preserves source TTLs and fields outside the SQL schema.
SQL DELETE removes the selected source HASHes, including their unmodeled fields.

The driver handles its index lifecycle, but source data is still mutable.
Queries can observe concurrent changes, multi-row writes can partially succeed,
and read-modify-write SQL expressions can lose intervening application updates.
Direct Redis writes must respect registered field encodings; they do not receive
SQL type/constraint validation. New source fields do not automatically become
SQL columns.

The mode supports SELECT, INSERT, UPDATE, DELETE, RETURNING, and Arrow ingestion with explicit creation/replacement configuration. It excludes shared managed snapshots, retained execution, MERGE,
TRUNCATE, schema-changing DDL, and mixed managed/live write statements. Ordinary
SELECTs may join live and managed tables; only the managed portion is frozen.
Collection queries use bounded key scans and can materialize results.

Remove the registration and its index without deleting the application data:

```sh
go/build/redis-adbc live unregister -table users -apply
```

SQL DROP TABLE is refused for live registrations. See the
[live HASH contract](live-hash-tables.md) for registration/resume, exact key rules,
nested-prefix index coverage, schema compatibility, and query limits.

### Arrow write operations

Table mode describes ownership; Arrow ingestion mode describes a write operation.
Managed ingestion has atomic supported create/append/replace behavior. Unmanaged
ingestion writes original source HASHes and has a separate partial-write contract;
source replacement requires explicit dedicated-source configuration and consent.
Live HASH tables accept `append` and `create_append` on existing registrations,
writing original HASHes with per-row acknowledgments. Explicit `live_source`
configuration enables creation under an empty prefix and dedicated replacement
with exact target/index and quiescence, TTL-loss and index-reset consent.
Omitting `__key` generates UUID keys;
see [live Arrow ingestion](live-hash-tables.md#write-arrow-data-to-live-hashes).

See the [unmanaged Arrow write contract](unmanaged-tables.md#write-arrow-data-to-source-hashes)
and [dedicated source replacement](unmanaged-arrow-replace.md) for the detailed
operation matrix and failure semantics.

## Managed imports are copies, not another ownership mode

A managed import connects application data to a separate driver-owned table.
It does not transfer ownership of the source HASHes/indexes or create a fourth
table mode.

By default, `AdoptHashes` creates a managed copy; repeating Apply for the same
source and destination explicitly refreshes it. `CopyHashes` also copies source
data into managed storage. Foreign-index discovery defaults to managed copies
unless its mode is explicitly set to unmanaged.

Source changes and TTL expiry do not change the managed copy. SQL writes to the
copy do not write back to the source. Refresh replaces the copy's captured rows
and schema, including private SQL changes since the previous capture. Managed
imports retain original source keys in hidden, read-only `__key` provenance;
SQL-added rows have NULL provenance.

Capture is not atomic with application writers. Pause source writes during
capture if a consistent source image is required. Once published, the managed
copy has the normal managed snapshot guarantees. Existing readers retain their
previous view until released or invalidated.

You can use application-owned source HASHes and managed analytical copies
together. That combines operational access with consistent reporting, at the
cost of duplicate storage and an explicit freshness/refresh policy. A table's
ownership mode never changes silently.

## Further reading

- [Live HASH registration, SQL/Redis access, and generated keys](live-hash-tables.md)
- [Unmanaged registration and application-owned indexes](unmanaged-tables.md)
- [Managed statement snapshots and storage lifecycle](snapshots.md)
- [Connection-scoped shared read snapshots](shared-read-snapshots.md)
- [Managed HASH import and refresh](driver-reference.md#import-or-refresh-a-managed-copy)
- [SQL authorization and ownership policies](sql-rbac.md)
