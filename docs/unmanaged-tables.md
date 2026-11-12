# Live unmanaged HASH tables

Unmanaged tables expose application-owned HASHes through SQL without copying
their data. Application changes, SQL writes and TTL expiry affect the same keys.
The source Search index determines visibility during scans. Managed copies
remain the default for adoption and foreign-index imports.

## Adopt a HASH collection

Create an application-owned HASH Search index on exactly the collection prefix,
without a FILTER. Then register it:

```sh
redis-adbc adopt -prefix app:user: -table users -index idx_users \
  -mode unmanaged -apply
```

Pass connection options using the CLI's normal connection flags. Without
`-apply`, the command plans and reports blockers without registering anything.
The Go API uses `HashAdoptOptions{Mode: "unmanaged", ...}`. Omitting Mode or
selecting `"managed"` retains managed-copy behavior.

This mode requires an existing compatible source index. `-own-index`,
`-index-columns` and `-trust-strings` are unsupported. The application owns the
index's configuration, memory and lifecycle. Adoption considers HASH keys under
the prefix with no further `:`; nested collections are excluded. Type, rename
and text/native encoding overrides remain available. Schema inference examines
the source; registration does not copy or change its HASHes.

## Register foreign indexes

Set both database options before opening the connection:

```python
db_kwargs = {
    "uri": "redis://localhost:6379/0",
    "adbc.redis.foreign_indexes": "true",
    "adbc.redis.foreign_indexes_mode": "unmanaged",
}
```

The mode option is also available on a connection; set it before enabling
`adbc.redis.foreign_indexes`. Tables appear in `redis_foreign` under their exact
index names. Quote punctuation, for example `redis_foreign."idx:users"`.
Multiple source prefixes and index FILTER membership are respected.

Foreign column inference samples up to 128 source HASHes and includes indexed
attributes. Incompatible values outside the sample can fail a query. Explicit
`adbc.redis.foreign_indexes_refresh=true` refreshes registered schema and source
definitions; later source values do **not** require refresh. Repeating unmanaged
adoption likewise refreshes the registration's schema. A changed source index
definition causes queries to fail until explicitly refreshed or re-registered.
Changing the registered source identity (its index name or primary prefix)
requires dropping the old registration first.

Existing tables cannot silently switch modes. DROP the old table/registration
first or choose another destination. Managed foreign imports remain read-only;
unmanaged foreign registrations permit source DML.

## Read and write the original keys

```sql
SELECT __key, name, age FROM users;
INSERT INTO users (__key, name, age) VALUES ('app:user:42', 'Ada', 37);
UPDATE users SET age = age + 1 WHERE __key = 'app:user:42';
DELETE FROM users WHERE __key = 'app:user:42';
DROP TABLE users;
```

`__key` identifies the original Redis key and is excluded from `SELECT *`.
INSERT requires a new explicit key in the registered source prefixes; an
existing key is refused. UPDATE preserves unrelated HASH fields and supported
text encodings; it does not recreate an expired/deleted HASH. Setting a HASH's
last field to NULL is refused because Redis would delete the row. DELETE
removes the source HASH, including its unmodeled fields, and skips a key that
has become a non-HASH value. UPDATE cannot rename `__key`.

For foreign indexes, INSERT is supported only without an index FILTER. UPDATE
and DELETE use the source index's membership, including filters and multiple
prefixes. Writes can change index membership according to the application
index's normal behavior. Native Redis ACLs must permit the required source
reads/writes and scripts; registration does not grant source-key access.

DROP removes only the table registration. Source HASHes, TTLs and Search indexes
remain application-owned and survive managed sweep/compaction. Foreign indexes
can be registered again by a later discovery/refresh.

## Consistency and supported operations

An unmanaged statement pins catalog metadata, **not source row values**. Scans,
joins and streamed results can observe intervening application changes or
expiry. Source Search indexing determines when changes become visible to scans.
A query mixing managed and unmanaged tables retains managed generations but
does not acquire a common point-in-time view of all data.

Source writes execute one HASH mutation at a time. Multi-row INSERT, UPDATE,
DELETE and MERGE can partially succeed. There is no rollback or atomic
publication for them; application and SQL writers must coordinate themselves.
The driver never automatically replays an uncertain source mutation. After an
error, inspect source state before retrying a write.

Unmanaged tables reject ALTER, TRUNCATE (including `truncate_adopted=true`),
managed index changes, Arrow ingestion, retained write preparation, shared read
snapshots, native row-ID reads and live partition reads. Data-changing WITH
statements cannot stage unmanaged writes inside managed publication. The
always-on managed snapshot option applies to managed tables only.

Choose a [managed copy](snapshots.md) when typed immutable generations,
statement snapshots or atomic SQL/Arrow publication are needed. See the
[ownership comparison](managed-vs-unmanaged-tables.md) for tradeoffs.
