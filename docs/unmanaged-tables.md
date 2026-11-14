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

<a id="append-arrow-data-to-source-hashes"></a>

## Write Arrow data to source HASHes

Use ADBC ingestion in `append` or `create_append` mode against an existing
unmanaged registration.
Every row must include a non-NULL string `__key` under the registered source
prefixes. Keys must be new: this is INSERT, not upsert. Logical column names,
types and native/text encodings follow the registration, including renamed
fields. A row must contain at least one non-NULL stored field.

```python
import pyarrow as pa

# conn is an ADBC connection; public.users is already registered as unmanaged.
rows = pa.table({"__key": ["app:user:43"], "name": ["Grace"], "age": [38]})
with conn.cursor() as cursor:
    written = cursor.adbc_ingest("users", rows, mode="append", db_schema_name="public")
```

Arrow input is converted in bounded slices and writes source HASHes directly.
There is no unmanaged publication step: each accepted row becomes visible as it
is written.
It does not create managed generations, acquire the managed Arrow append gate,
or freeze application data. Existing HASHes, unrelated fields and TTLs are not
overwritten. INSERT through filtered foreign indexes, Arrow `replace`, shared
read snapshots and retained/staged writes are unsupported.
Schema-level refusals occur before consuming the stream; row values and keys
are validated as each conversion slice is reached.

Once an unmanaged Arrow write passes target/schema preflight, a conversion, stream,
key-conflict, cancellation or Redis error leaves earlier successful rows in
place. The ADBC error message and UTF-8 error detail
`adbc.redis.ingest.acknowledged_rows` report the number of acknowledged rows;
Go ExecuteUpdate/ExecuteQuery also return that count with the error. This is a
**lower bound**, since a lost reply can hide applied rows in a submitted pipeline.
The count includes all acknowledged successes and need not describe a contiguous
prefix of input. INSERT uses pipelines of at most 128 single-key scripts with a
1 MiB request target and a 64 MiB single-row ceiling. After a conflict,
cancellation or lost reply, already-submitted rows may still apply; no further
pipeline is submitted. The original ADBC status and SQLSTATE are retained. Failures before target resolution or
unsupported-operation admission may have the usual unknown count instead of a
progress detail; input has not been consumed. No input or uncertain mutation is
replayed.
Inspect source state before retrying; there is no rollback or whole-stream
atomicity. This API does not require the managed snapshot `NativeClient` wrapper.

### Create a new unmanaged collection

Provide the statement option `adbc.redis.ingest.unmanaged_source` to select
unmanaged creation. Its JSON requires an explicit application `prefix` and
`index` name; optional `encodings` maps public column names to `native` or `text`.
Native storage is the default. Text encoding is supported for boolean, date and
time columns. All input rows still require explicit string `__key` values.

```python
import json

with conn.cursor() as cursor:
    cursor.adbc_statement.set_options(**{
        "adbc.redis.ingest.unmanaged_source": json.dumps({
            "prefix": "app:user:",
            "index": "app_users_membership",
            "encodings": {"enabled": "text"},
        })
    })
    rows = pa.table({"__key": ["app:user:1"], "name": ["Ada"], "enabled": [True]})
    cursor.adbc_ingest("users", rows, mode="create", db_schema_name="public")
```

`create` requires an unused table name, an empty prefix across all key types
(including nested keys), and an unused index name. The Arrow schema defines the
public fields and SQL types; `__key` is input-only. The driver creates a minimal
application-owned Search index for row discovery: one `TAG NOINDEX` attribute,
no hidden row-ID fields, and no field indexes for predicate pushdown. Creation
uses `SKIPINITIALSCAN` and rechecks prefix emptiness after index creation, so a
concurrent row that might have missed indexing causes refusal before registration
or input consumption. SQL
predicates continue to evaluate authoritative HASH values. The application can
manage its Search indexes; the managed `ingest.index_columns` override is not
supported for this creation path.

`create_append` creates when the registration is missing and otherwise appends
using its existing schema, index and encodings. An existing unmanaged target
accepts `create_append` without the source option. When the option is supplied,
its prefix/index and any explicitly supplied encodings must match the existing
registration. A managed target cannot silently switch ownership modes.

Creation coordinates **registration metadata** before consuming input. It commits
the registration before writing rows, then releases creation admission. Data
writes have no publication step. Failure can leave the new application index,
registration and acknowledged partial rows; these resources are deliberately
preserved, including after a lost creation reply or cancellation. Inspect them
before retrying. To use an already existing source/index, explicitly register it
with unmanaged adoption and then append. There is no automatic replay, rollback,
index deletion or whole-call atomicity. Coordinate external writers during
creation; the empty-prefix check does not freeze application activity.

The statement option persists until cleared. Set it to the empty string before
reusing the statement to create a managed table. With no source option, new Arrow
tables use the managed default. Temporary/virtual targets, retained preparation
and staged writes are unsupported. Arrow `replace` remains deferred in
[issue #399](https://github.com/redis/redis-adbc-driver/issues/399).

## Consistency and supported operations

An unmanaged statement pins catalog metadata, **not source row values**. Scans,
joins and streamed results can observe intervening application changes or
expiry. Source Search indexing determines when changes become visible to scans.
For compatible native integer NUMERIC attributes, Search narrows candidate rows
and SQL rechecks the complete predicate against current HASH values. Other
attribute types and text encodings retain client evaluation. Source indexing lag
can affect predicate candidate discovery as well as membership. Vanished/non-HASH
keys are skipped when fetching live HASHes after discovery.
Unfiltered full scans retain `LOAD *`; filtered unordered scans can fetch live
HASHes after Search key discovery. Sorting and value aggregates do not trust
mutable index values.
A query mixing managed and unmanaged tables retains managed generations but
does not acquire a common point-in-time view of all data.

Each source HASH mutation is individually atomic. INSERT and Arrow append use
bounded pipelines; UPDATE and DELETE issue one mutation at a time. Multi-row
INSERT, UPDATE, DELETE, MERGE and Arrow append can partially succeed. There is no
rollback or atomic publication for them; application and SQL writers must coordinate
themselves.
The driver never automatically replays an uncertain source mutation. After an
error, inspect source state before retrying a write.

Unmanaged tables reject ALTER, TRUNCATE (including `truncate_adopted=true`),
managed index changes, Arrow replace, retained write
preparation, shared read snapshots, native row-ID reads and live partition reads.
Data-changing WITH statements cannot stage unmanaged writes inside managed publication. The
always-on managed snapshot option applies to managed tables only.

Choose a [managed copy](snapshots.md) when typed immutable generations,
statement snapshots or atomic SQL/Arrow publication are needed. See the
[ownership comparison](managed-vs-unmanaged-tables.md) for tradeoffs.

## Experimental Software Flex stopgap

Unmanaged registrations work with `adbc.redis.flex=true` on compatible Redis
Software Flex deployments. Both entry paths are supported: unmanaged HASH
adoption and foreign indexes with `adbc.redis.foreign_indexes_mode=unmanaged`.
For example:

```sh
redis-adbc adopt -prefix app:user: -table users -index idx_users \
  -mode unmanaged -option adbc.redis.flex=true -apply
```

Use the normal URI/TLS connection flags. For foreign discovery, set the Flex,
foreign-index and unmanaged-mode database options before Open, or set unmanaged
mode on the connection before enabling foreign indexes. Managed-copy foreign
imports remain unsupported on Flex; an enabled Flex connection cannot switch
foreign discovery to managed mode.

The application must supply a ready, compatible HASH Search index. The driver
never creates, repairs, reindexes or takes ownership of that index. Registration
verifies singleton `FT.SEARCH ... INKEYS` support. Reads enumerate source HASHes
using `SCAN`, check each candidate's index membership, and fetch authoritative
HASH values. Even direct `__key` lookups check membership. This preserves index
FILTERs, multiple/overlapping prefixes and adopted collections' nested-key
exclusions; HASHes omitted by the application index remain invisible. Source
indexing lag, application changes and expiry retain ordinary live semantics.

Enumeration requires SCAN permission; reads also require FT.INFO, FT.SEARCH and
HASH read permissions. The stopgap's normal temporary capability-probe
permissions also apply. Membership checks use pipelines of at most 128 singleton
queries, with a 1 MiB request target and a 64 MiB single-key ceiling. Each query
returns a count, so data enumeration does not depend on Search result caps,
aggregate cursors, sortable fields or numeric driver row IDs. Deduplication
retains one entry per observed key: memory grows with source key count. Sorting
and SQL evaluation run in the client; no performance parity or constant-memory
claim is made. Source ACLs, native encodings, TTLs, non-replayable writes and
DROP/GC ownership rules above remain in force, as do the unmanaged operation
exclusions.

See [implementation and live validation](sdlc/changes/flex-unmanaged/validation.md).

Filtered reads can fetch only required HASH fields after Search discovers keys.
The driver uses a bounded, per-key read-only Lua TYPE/HMGET operation to preserve
live existence and exact native values, including existing rows whose selected
fields are all NULL. Full scans retain their established retrieval strategy.
The additional `EVALSHA_RO`, `EVAL_RO`, `TYPE` and `HMGET` permissions enable this
acceleration; a definite permission refusal retains the existing HGETALL path.
Unknown command outcomes fail without replay. No application HASH/index is
modified, and permissions or values are not cached across statements.
