# Managed catalogs

The default catalog is `redis`; its default schema is `public`. Additional
managed catalogs have independent schemas and tables within the same Redis
endpoint and immutable managed metadata root. They are not Redis numbered
databases or Redis security boundaries.

```sql
CREATE DATABASE analytics;
CREATE TABLE analytics.public.events (id BIGINT, message TEXT);
INSERT INTO analytics.public.events VALUES (1, 'ready');
SELECT * FROM analytics.public.events;
DROP DATABASE analytics CASCADE;
```

`CATALOG` is an alias for `DATABASE`. Creation accepts `IF NOT EXISTS`; dropping
accepts `IF EXISTS` and `CASCADE` or `RESTRICT`. Every new catalog has a `public`
schema. Without `CASCADE`, a catalog with nonempty schemas cannot be dropped.
The `redis` catalog and the connection's currently selected catalog cannot be
dropped. Select a different catalog before dropping it.

Use the ADBC `adbc.connection.catalog` connection option to select a catalog.
Selection is local to the connection and resets its current schema to `public`.
`adbc.connection.db_schema` selects a logical schema within that catalog.
Unqualified SQL names use that catalog and schema; `current_database()` and
`current_schema()` report them. Catalog, schema and table can also be supplied
explicitly in three-part SQL references.

Arrow ingestion accepts `adbc.ingest.target_catalog` independently of the
connection's current catalog, alongside its target schema and table. Create,
append, create-append and replace use the selected destination. Replacement
changes only that catalog's target; identical names elsewhere remain separate.
Unknown catalog destinations fail before reading Arrow input. Select a catalog
again if another connection drops and recreates its name; an old selection
cannot silently target the replacement catalog.

ADBC catalog/schema/table discovery, `GetTableSchema` and statistics accept
logical catalog names. SQL `information_schema` and `pg_catalog` describe the
current catalog and display logical schema names. Select the catalog before
querying its virtual system schemas; cross-catalog virtual system schema queries
are unsupported. Managed row prefixes and internal catalog identities are not
SQL schema names and cannot be supplied as schema options or quoted identifiers.

Temporary objects, live tables in `redis_live`, and application-owned foreign
indexes in `redis_foreign` remain available only in `redis`. Catalog-qualified
renames must stay within the same catalog and schema. Catalogs share Redis ACL
and resource configuration; existing managed table ownership, immutable snapshot
publication and prefix-based RBAC checks still apply.

Named catalogs currently reject deferred partition production/reading, retained
query execution, and connection-bound shared read snapshots. Ordinary managed
statement snapshots continue to protect named-catalog SQL and Arrow operations.
These advanced features remain available in `redis`; selecting or explicitly
referencing a named catalog cannot bypass their restriction.
