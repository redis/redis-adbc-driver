# Live HASH tables

This feature requires a source build of this checkout.

Live tables are a third ownership mode: **your application owns the HASHes; the
driver owns the SQL definition and its Search index**. SQL and Redis clients
read and write the same keys. There is no managed data copy and no refresh step.

| | Managed tables and imports | Unmanaged application data | Live HASH tables |
|---|---|---|---|
| Data owner | Driver | Application | Application |
| Index manager | Driver | Application or another tool | Driver |
| Direct Redis edits | Unsupported | Yes | Yes |
| SQL reads and writes | Yes | Yes, with explicit unmanaged registration and a compatible application index | Yes, against original HASHes |
| Consistency | Managed statement snapshots and atomic supported writes | Application's Redis operations | Live reads and per-row writes; concurrent changes can interleave |
| Data duplication | Imports make a private copy | None required | None |

## Register a collection

Start with existing HASHes under an application prefix, for example
`app:users:alice` with fields `name` and `age`. Build the command-line tool with
`make -C go adbc-cli`, and use your configured `REDIS_URI` for the server.

```sh
go/build/redis-adbc live register -prefix app:users: -table users
go/build/redis-adbc live register -prefix app:users: -table users -apply
```

The first command previews the inferred SQL columns. The second registers the
collection and creates its index. Existing HASHes are scanned to establish a
fixed schema; registering an empty collection without inferable fields is not
supported. `-type`, `-rename`, `-encoding` and `-index-columns` provide the same
column shaping controls as HASH adoption. New HASH fields do not automatically
become SQL columns.

Enable `adbc.redis.live_tables=true` as a database or connection option. Registered
tables appear in the `redis_live` schema. That schema name must not already be
used by a managed schema. The name is reserved for new schemas. There can be
at most 1,024 registrations, each with at most 1,024 columns and a 1 MiB
definition. Prefixes and column names must be valid UTF-8; row keys may contain
binary suffixes.

```sql
SELECT __key, name, age FROM redis_live.users;

INSERT INTO redis_live.users (__key, name, age)
VALUES ('app:users:bob', 'Bob', 40);

UPDATE redis_live.users SET age = 41
WHERE __key = 'app:users:bob';

DELETE FROM redis_live.users WHERE __key = 'app:users:bob';
```

`__key` is the original Redis key. INSERT requires it and refuses an existing
key, including one created concurrently. A key under the prefix with another
colon after that prefix belongs to a nested collection and is excluded. No
internal row ID or version fields are added to the HASHes.

An application can use ordinary `HGETALL`, `HSET`, `HDEL`, `DEL` and expiry on those
same keys. Updates through SQL preserve the key's TTL and unrelated fields.
SQL NULL removes the corresponding field; an UPDATE that would remove the last
field is refused because Redis would delete the entire HASH. DELETE deliberately
deletes the selected source HASHes.

## Consistency and schema

Live tables have no managed statement snapshot. Queries, joins and cursor pages
can observe changes made while they run. Search-based reads follow the index's
visibility; an exact `__key` lookup fetches the current HASH directly. SQL updates
are atomic per row, but a statement affecting many rows can partially apply
before a failure. A lost response can leave a row's outcome uncertain. Do not
automatically retry a failed write unless the application can resolve its effect.

SQL expressions are evaluated from the matched row. A concurrent writer can
change it before the SQL write, so read-modify-write expressions such as
`age = age + 1` can lose an intervening update. Deleting and recreating a key does
not give it a new identity visible to the driver. An UPDATE does not recreate a
missing HASH; a row that disappears during mutation causes a conflict error,
and earlier row changes can remain applied. These rules differ from managed
statement publication.

Direct writes must keep the registered field encodings and SQL types valid.
They bypass SQL validation. Index failures or an unavailable/rebuilding index
cause queries to fail rather than silently accept that index as complete. Field
names beginning with `__` are reserved. Do not change or delete driver-owned
indexes or control metadata directly.

This initial mode supports SELECT, INSERT, UPDATE, DELETE and RETURNING. SQL
writes may read other live tables, but cannot read managed tables in the same
write statement. Managed writes cannot consume live tables. Shared managed read
snapshots, retained execution, Arrow ingestion, MERGE, modifying CTEs, TRUNCATE
and schema-changing DDL are not supported for live tables. Ordinary SELECTs may
join live and managed tables; only the managed portion is frozen.

## Remove or resume a registration

```sh
go/build/redis-adbc live unregister -table users -apply
```

This removes the driver's index and registration, **leaving every source HASH
unchanged**. SQL `DROP TABLE` is refused for this schema; use unregister so the
ownership boundary is explicit. Active queries can fail if their registration
is removed. Managed compaction and reclamation never own these application rows.

An interrupted registration retains its generated index identity. When the
index exists and is healthy, finish publication without repeating index creation:

```sh
go/build/redis-adbc live resume -table users -apply
```

An existing unfinished index can also be removed with unregister. If a create
outcome is unknown and its index cannot be observed, the recovery identity is
retained; absence alone does not authorize replaying that create. An indexing
failure requires fixing the source encodings, unregistering the failed index,
and registering again. Ordinary Redis writes maintain a healthy index without
manual index updates.

The current live query path scans keys in bounded batches, uses index candidate
checks to avoid unnecessary HASH fetches, and evaluates predicates, ordering and
aggregates against fetched values. It does not promise faster queries than
managed tables. Unordered live collection queries currently use the ordinary
materializing fallback; `stream_required=true` is unsupported for these scans.
The Search index covers all HASHes under the prefix, including nested keys which
SQL excludes. Indexed fields on those nested HASHes must also have compatible
encodings, or Search can report an indexing failure for the collection.

Go callers use `RegisterLiveHashes`, `ResumeLiveHashes` and
`UnregisterLiveHashes`. `AdoptHashes`, `CopyHashes` and foreign-index imports keep
their existing managed-copy behavior. See the [ownership comparison](managed-vs-unmanaged-tables.md)
and [managed statement snapshots](snapshots.md).
