# Replace a dedicated unmanaged HASH collection

Unmanaged Arrow `replace` changes application-owned data directly. Managed
replacement publishes a complete schema/data generation atomically and leaves
import sources untouched. Unmanaged replacement has no whole-call atomicity,
rollback, source snapshot or process resume.

## Explicit scope and consent

Set the statement's `adbc.redis.ingest.unmanaged_source` option with the exact
registered prefix/index and a `replace` object naming the exact schema/table.
Every boolean below must be `true`. This option persists until cleared. A
`replace` object is refused in other ingest modes.

```python
import json
import pyarrow as pa

# Stop all writers and DDL users of this dedicated collection first.
with conn.cursor() as cursor:
    cursor.adbc_statement.set_options(**{
        "adbc.redis.ingest.unmanaged_source": json.dumps({
            "prefix": "app:user:",
            "index": "app_users_membership",
            "replace": {
                "schema": "public",
                "table": "users",
                "exclusive_quiescent": True,
                "discard_ttls": True,
                "reset_index": True,
            },
        }),
    })
    rows = pa.table({"__key": ["app:user:42"], "name": ["Ada"]})
    written = cursor.adbc_ingest("users", rows, mode="replace", db_schema_name="public")
```

`exclusive_quiescent` attests administrative exclusive use of both prefix and
index and that all application/driver writers and DDL users remain stopped from
preflight through completion. A matching prefix or registration alone proves
neither ownership nor exclusivity. The driver does not install a distributed
source lock or fence arbitrary Redis clients. Observed interference stops the
operation; arbitrary concurrent writes cannot be reliably detected. An operator
who cannot establish exclusivity must use a new prefix/index and coordinate an
application cutover instead.

The target must already be an unmanaged, permanent, non-foreign registration
with exactly one unfiltered HASH index prefix matching the configuration. Other
overlapping indexes, unreadable index definitions, another registered consumer
of the prefix/index, unmodeled or aliased index attributes, non-HASH keys and
nested keys are refused. The prefix itself is not a valid row key. A row key's
suffix must contain no `:`. Keys are byte strings: prefixes are matched literally,
including glob characters and NUL bytes; there is no path normalization.
Metadata namespaces remain excluded. Shared, filtered, multi-prefix, retained,
temporary, staged and shared-snapshot replacements are unsupported.

Scope, target identity, Arrow schema and command permissions are checked before
calling Arrow `Next` or modifying source data. Permission checks use the actual
script principal via `redis.acl_check_cmd`; they cannot guarantee permissions for
unseen input keys or prevent later permission changes. Required source commands
include SCAN, EVAL, TYPE, DUMP, PEXPIRETIME, EXISTS, DEL and HSET; Search requires
FT._LIST, FT.INFO, FT.DROPINDEX and FT.CREATE, plus normal catalog permissions.

## Data, schema, TTLs and indexes

Every input row requires a unique non-NULL string `__key` within the dedicated
collection and at least one non-NULL public field. The supplied Arrow schema
replaces the public schema; optional `encodings` describes the new schema using
the same native/text rules as unmanaged creation. Managed `ingest.index_columns`
is refused. Row values, duplicates and key scope are checked while streaming.

All inventoried old HASHes are deleted, including keys absent from the new input,
unmodeled fields and old TTLs. Input rows become new complete persistent HASHes,
including rows reusing an old key. No TTL or unmodeled-field preservation is
offered. An empty input removes all old rows and retains the supplied schema.

`reset_index` authorizes dropping only the named index **without DD**, then
creating a membership-only `TAG NOINDEX` index on the new schema's first public
field. Old attribute types/options, aliases, SORTABLE settings and other index
configuration are discarded; field pushdown indexes are not recreated. No
hidden managed fields are installed. This changes every consumer of that index,
which is why exclusive-use attestation is required. Other indexes are preserved.

## Execution and limits

1. Pin and validate registration/configuration; inspect all source keys using
   SCAN across primaries, independently of Search membership. Retain deduplicated
   key/DUMP/absolute-expiry observations sorted by exact key bytes. Inventory is
   bounded by 100,000 keys and 64 MiB including a per-entry allowance. Exceeding
   either bound refuses admission before mutation/input consumption. SCAN cursors
   exist only for this call and are never persisted.
2. Delete one inventoried key per atomic script, only if its type, DUMP image and
   absolute expiry still match. An observed change or expiry stops deletion.
   Recheck current registration and index definition before each delete. Require
   the entire prefix to be empty afterward; never broaden the old inventory to
   delete newly observed keys.
3. Drop/reset the dedicated index, require the prefix still empty, and update
   registration metadata only if it still matches the admitted registration.
   Catalog CAS conflicts stop rather than retarget or replay the operation.
4. Consume Arrow once using existing unmanaged INSERT conversion and bounded
   pipelines. Recheck current registration/index definition before each pipeline.
   New keys must be absent. Input key tracking has a 100,000-key / 64 MiB bound.
5. Recheck registration/index and independently inventory the final source keys.
   The final inventory has the same 100,000-key / 64 MiB bound and requires exactly
   the input key set, HASH types and persistent TTLs. It does not compare every
   new value against a retained copy of the input. Concurrent raw changes remain
   outside the contract even when unobserved. A final inventory overflow is a
   partial failure after writes, not a successful replacement.

Readers can see missing/old/new rows or temporary index/schema mismatches. The
new schema registration becomes visible before new rows are streamed. There is
no atomic commit across HASH mutation, Search DDL and catalog publication.
Search's ordinary indexing lag remains visible; the final inventory is a source
key/type/TTL check, not a promise of simultaneous Search query visibility.

## Failure details and recovery

Cancellation, Close, conversion/reader errors, permission loss, conflicts and
lost replies stop further input/mutation submission. Already submitted INSERT
pipeline rows may apply. No input, uncertain delete, index DDL or row mutation is
automatically replayed. Original ADBC status and SQLSTATE are retained.

Failures after replacement admission return the acknowledged write count through
Go ExecuteUpdate/ExecuteQuery and attach UTF-8 ADBC error details:

| Detail | Meaning |
|---|---|
| `adbc.redis.ingest.acknowledged_deletes` | Confirmed old HASH deletes |
| `adbc.redis.ingest.acknowledged_rows` | Confirmed new row writes |
| `adbc.redis.ingest.phase` | `preflight`, `delete`, `drop_index`, `create_index`, `registration`, `write` or `verify` |
| `adbc.redis.ingest.current_outcome` | `none`, `acknowledged` or conservatively `unknown` for the latest submitted mutation/pipeline |

Counts are lower bounds; zero acknowledged deletes/writes does not prove no
mutation applied. A lost index-create or registration-publication reply can
leave that operation applied. Errors before entering this explicit replacement
path can retain the usual unknown count. No successful return is reported until
the schema registration, index definition and complete source inventory checks
finish.

Recovery belongs to the operator. Keep writers stopped, inspect source keys,
schema registration, index definition and TTLs against a trusted backup/input
manifest, and reconcile them explicitly. Restore the old state or finish a
separately authorized operation with fresh input. Never assume retrying the same
stream is safe. DROP and managed sweep/GC preserve all application-owned HASHes
and indexes, including remnants of failed replacement.

See [implementation evidence](sdlc/changes/issue-399-unmanaged-replace/validation.md)
and the [historical proposal](unmanaged-arrow-replace-proposal.md).
