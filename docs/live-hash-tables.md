# Live HASH tables

Live HASH tables and their Arrow ingestion modes are included in
[v0.0.30](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.30) and later.

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

-- Omit __key to generate app:users:<uuid> with GEN_RANDOM_UUID().
INSERT INTO redis_live.users (name, age)
VALUES ('Bob', 40) RETURNING __key;

-- An explicit Redis key is also supported.
INSERT INTO redis_live.users (__key, name, age)
VALUES ('app:users:carol', 'Carol', 42);

UPDATE redis_live.users SET age = 41
WHERE __key = 'app:users:carol';

DELETE FROM redis_live.users WHERE __key = 'app:users:carol';
```

`__key` is the original Redis key. Omitting it, or specifying `DEFAULT`, uses
the SQL default `<registered-prefix> || GEN_RANDOM_UUID()::TEXT`, evaluated
separately for every inserted row. Use `RETURNING __key` to retrieve generated
keys. An explicit key is preserved, explicit NULL is rejected, and an existing
key is refused, including one created concurrently. [Unmanaged SQL INSERTs](unmanaged-tables.md#read-and-write-the-original-keys)
also generate prefixed UUID keys when `__key` is omitted or supplied as `DEFAULT`; unmanaged Arrow
ingestion requires explicit keys. A key
under the prefix with another colon after that prefix belongs to a nested
collection and is excluded. No
internal row ID or version fields are added to the HASHes.

An application can use ordinary `HGETALL`, `HSET`, `HDEL`, `DEL` and expiry on those
same keys. Updates through SQL preserve the key's TTL and unrelated fields.
SQL NULL removes the corresponding field; an UPDATE that would remove the last
field is refused because Redis would delete the entire HASH. DELETE deliberately
deletes the selected source HASHes.

## SQL and Redis client example

Use the Redis fixture and Python environment from the
[README quick start](../README.md#quick-start), with the virtual environment
active and connection environment loaded. Use v0.0.30 or later, or a current
source build. Register the example collection once.
Install the Redis Python client:

```bash
python -m pip install redis
```

This example creates an application HASH, registers its collection, inserts a row
through SQL, and edits that same row through a Redis client. The driver creates
and maintains the Search index.

```python
import os
import subprocess
import redis
from adbc_driver_redis import dbapi

uri = os.environ["REDIS_URI"]
redis_options = {"decode_responses": True}
if uri.startswith("rediss://") and os.environ.get("SSL_CERT_FILE"):
    redis_options["ssl_ca_certs"] = os.environ["SSL_CERT_FILE"]
r = redis.Redis.from_url(uri, **redis_options)
r.hset("demo:users:seed", mapping={"name": "Ada", "age": 37})

# Register once; the application HASHes stay where they are.
subprocess.run(
    ["go/build/redis-adbc", "live", "register",
     "-prefix", "demo:users:", "-table", "users", "-apply"],
    check=True, stdout=subprocess.DEVNULL,
)

with dbapi.connect(uri, db_kwargs={"adbc.redis.live_tables": "true"}) as conn:
    with conn.cursor() as cur:
        cur.execute("""
            INSERT INTO redis_live.users (name, age)
            VALUES ('Grace', 40) RETURNING __key
        """)
        key = cur.fetch_arrow_table()["__key"][0].as_py()
        print(r.hgetall(key))          # Read the SQL-created HASH directly.
        r.hset(key, "age", 41)         # Edit it with a Redis client.
        cur.execute("SELECT name, age FROM redis_live.users WHERE __key = ?", (key,))
        print(cur.fetch_arrow_table()) # SQL reads the updated value.
```

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
snapshots, retained execution, MERGE, modifying CTEs, TRUNCATE
and schema-changing DDL are not supported for live tables. Ordinary SELECTs may
join live and managed tables; only the managed portion is frozen.

## Write Arrow data to live HASHes

Enable `adbc.redis.live_tables=true` and target `redis_live`. Arrow batches and
streams support all four ingest modes. `append` requires an existing registration;
`create` and creation through `create_append` require an explicit empty application
prefix. `replace` requires an existing dedicated registration and explicit consent.
Append preserves the registered schema and index policy.

```python
import os
import pyarrow as pa
from adbc_driver_redis import dbapi

with dbapi.connect(
    os.environ["REDIS_URI"],
    db_kwargs={"adbc.redis.live_tables": "true"},
) as conn, conn.cursor() as cur:
    # Assumes app:users: is already registered as redis_live.users.
    count = cur.adbc_ingest(
        "users",
        pa.table({"name": ["Dana", "Eli"], "age": [28, 35]}),
        mode="append",
        db_schema_name="redis_live",
    )
```

Column names map to the registered SQL schema, including any source-field
renames. Unknown or duplicate columns are rejected. Omitted public columns are
NULL; registered types and encodings still apply. Include a string `__key`
column for explicit keys, or omit it to evaluate the same prefixed
`GEN_RANDOM_UUID()` default used by SQL INSERT for every row. NULL keys, keys
outside the registered prefix, nested keys, existing keys and duplicate keys
are errors. At least one public field must be non-NULL so a HASH can exist.
The virtual key and internal row fields are never stored in the HASH.
Ingestion returns an affected-row count; use SQL INSERT with `RETURNING __key`
if you need generated keys returned directly.

Writes use bounded conversion slices and non-replaying pipelines of single-key
atomic inserts. They have the same per-row, partial-write semantics as live SQL
writes. Existing HASHes, TTLs, fields outside the schema, registrations and indexes
are preserved. An error can leave earlier rows applied, including later successful
rows in the already submitted pipeline. The error detail
`adbc.redis.ingest.acknowledged_rows` counts positive acknowledgments across that
pipeline and earlier ones. A lost reply can leave additional rows applied, so
the count is a lower bound; do not blindly retry the input after a failure.
Shared snapshots, retained execution, temporary targets and
`adbc.redis.ingest.unmanaged_source` configuration cannot be used for live
ingestion.

### Create a live collection from Arrow

Set `adbc.redis.ingest.live_source` on the statement before ingesting. The option
is JSON and persists until cleared with an empty string. Creation requires an
empty application prefix, including no nested or non-HASH keys. The Arrow schema
must contain at least one public column; `__key` is optional and must be a string
when supplied. The driver creates its own generated Search index and registration
before consuming batches. `create_append` uses this configuration only to create
an absent registration; on an existing registration its prefix/encodings must match.

```python
import json

cur.adbc_statement.set_options(**{
    "adbc.redis.ingest.live_source": json.dumps({"prefix": "app:new_users:"}),
})
cur.adbc_ingest(
    "new_users", pa.table({"name": ["Ada"], "age": [37]}),
    mode="create", db_schema_name="redis_live",
)
```

Optional `encodings` maps column names to `native` or `text` (text is supported
for boolean/date/time columns). `adbc.redis.ingest.index_columns` controls which
public fields the new driver-owned index includes. Empty Arrow input creates an
empty registered collection with the supplied schema. Existing application data
must use ordinary live registration instead. No hidden fields are written.

Creation is not atomic across registration, Search DDL and source rows. A failure
can leave a `building` registration, generated index or partial input. Inspect
these resources before retrying; use live resume/unregister for registration
recovery. Neither recovery command replays Arrow input. Unregister preserves
application HASHes. A competing registration is never refreshed by create.

### Replace a dedicated live collection

Stop all application and driver writers and DDL users first. Set the exact
registered prefix and current driver-owned index, plus every consent below:

```python
cur.adbc_statement.set_options(**{
    "adbc.redis.ingest.live_source": json.dumps({
        "prefix": "app:new_users:",
        "index": current_live_registration_index,
        "replace": {
            "schema": "redis_live",
            "table": "new_users",
            "exclusive_quiescent": True,
            "discard_ttls": True,
            "reset_index": True,
        },
    }),
})
cur.adbc_ingest(
    "new_users", pa.table({"name": ["Grace"]}),
    mode="replace", db_schema_name="redis_live",
)
```

Obtain the index identity from the live registration result. Consent persists
with the option and is refused in other modes. Replacement deletes every old
HASH in the dedicated collection, including unknown fields and TTLs, then resets
the named index **without DD**, stages the new registration/schema, creates its
index and streams new rows.
An omitted `__key` generates fresh UUID keys. Empty input removes all old rows and
retains the supplied schema. The new index uses the supplied schema and optional
`ingest.index_columns`; it remains driver-owned.

Replacement uses the [dedicated source replacement](unmanaged-arrow-replace.md)
inventory, image/expiry-checked deletes, overlap/consumer checks, permission
preflight, 100,000-key / 64 MiB inventory and input-key limits, final source-key
verification and no-replay rules. Live replacement also validates the driver-owned
index definition. Renamed source-field aliases, nested keys, non-HASH keys,
overlapping indexes or another registered consumer are refused. The operator must
keep the collection exclusively quiescent throughout; arbitrary Redis writers
cannot be fenced. Shared snapshots, retained/staged execution and temporary
collections are unsupported.

Readers can observe old, missing and new rows or temporary schema/index mismatch.
There is no whole-call rollback or process resume. On failure, inspect source
HASHes, TTLs, index and registration against trusted input/backup before recovery.
Before index creation, the replacement schema is retained as a `building`
registration. If index creation applied but its reply was lost, ordinary live
resume can publish that schema; it does not restore deleted data or replay input.
Error details include `adbc.redis.ingest.acknowledged_deletes`,
`adbc.redis.ingest.acknowledged_rows`, `adbc.redis.ingest.phase` and
`adbc.redis.ingest.current_outcome`. Counts are lower bounds after lost replies;
uncertain deletes, index commands and input writes are never replayed.

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
