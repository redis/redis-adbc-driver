# Deferred unmanaged HASH partitions

Set the statement option `adbc.redis.partition_source_mode=unmanaged` to use
`ExecutePartitions` / `ReadPartition` on an existing unmanaged registration.
The default remains `managed`; this opt-in does not copy or mutate source data.
The application owns both the HASHes and their Search index. The result mode
stays `live` (the default deferred ADBC mode), and `partition_count` controls the
number of disjoint key buckets, not the number of automatically started workers.
The descriptor set has a 64 MiB cumulative encoded-byte ceiling in addition to
the one-MiB per-descriptor limit; oversized query/parameter duplication is refused
before constructing the set.
The application schedules readers; the internal query-worker budget does not cap
these external tasks. Existing catalog limits (including 128 live group/reader
pins) still apply across the database.

```python
with conn.cursor() as cursor:
    cursor.adbc_statement.set_options(**{
        "adbc.redis.partition_source_mode": "unmanaged",
        "adbc.redis.partition_count": 4,
    })
    partitions, schema = cursor.adbc_execute_partitions(
        "SELECT CAST(__key AS BYTEA), amount FROM public.orders WHERE amount > 0"
    )
# Send each opaque descriptor to independently authenticated ReadPartition workers.
```

Use the existing [row-local partition query subset](partitioned-reads.md#supported-queries):
one permanent registered table, deterministic projections and predicates, with
optional typed single-row parameters. Joins, aggregates, views, global ordering,
LIMIT/OFFSET and managed balanced row-ID planning are rejected. The explicit
source selector cannot be combined with retained result mode. Compatible Redis
Software Flex works with its required experimental option; workers need no
aggregate cursor or SORTABLE attributes.

## Splits and cost

A version-5 descriptor assigns one bucket using SHA256 over every raw Redis key
byte, including NUL and invalid UTF-8, without normalizing it. The first eight
digest bytes form an unsigned big-endian number; remainder modulo partition count
chooses the bucket. Every worker scans all registered source prefixes, then reads
only its bucket's index members. SCAN duplicates and overlapping prefixes are
deduplicated inside each worker. Empty/skewed buckets remain valid.

Each worker repeats SCAN for every registered prefix. MATCH can traverse unrelated
database keys too, so discovery can cost O(partitions × prefixes × database keys),
not just the number of returned source rows. More workers can make it slower. This feature supplies schedulable tasks; it does not assume improved
throughput. The [change evidence](sdlc/changes/issue-549-unmanaged-partitions/verification.md)
records actual checks and measurement limits.

Each worker permits up to 100,000 unique assigned keys, 64 MiB of logical retained
key/map credits, one-MiB source keys, 256 prefixes, one million SCAN calls and 2^31
observed key replies. Limits are checked before retaining deduplication entries.
An excess is an explicit resource error (SQLSTATE 53200); discard partial output
from a failed reader. SCAN COUNT is a hint, and these credits are not native reply,
Arrow value, Redis memory or process RSS ceilings. Stream batch settings retain
their ordinary meaning. Increase partition count or narrow source registrations
when assigned-key credits are insufficient; more partitions repeat discovery.

## Values, membership and lifetime

Native singleton FT.SEARCH INKEYS counts enforce source index membership,
including FILTER and multiple prefixes. HASHes are authoritative for values and
SQL rechecks the full predicate. Source keys/unknown fields/TTLs/indexes remain
unchanged. A key omitted by the native application index is omitted by partitions,
even if its HASH fields would appear to satisfy the index FILTER. Some tested
Software filtered-index fixtures omit NUL-bearing keys; this is recorded native
behavior, not permission to invent membership. Indexed non-UTF8 keys retain their
exact bytes; project `CAST(__key AS BYTEA)` for keys that are not UTF-8 text.

Source values remain live. Application writes, TTL expiry and indexing lag may
interleave with enumeration, membership checks and HASH retrieval. A fixed source
has exact nonduplicated membership across the whole descriptor set. Under changes,
there is no common point-in-time source view and retrying can return different
rows/values. A vanished or retyped HASH is omitted after exact native evidence;
unknown/composed reply failures are terminal errors, without automatic fallback
or replay. Native SCAN, FT.INFO, FT.SEARCH and HASH read permissions are required.

The existing partition group pins only catalog/registration metadata. A later
registration refresh/drop does not rewrite that pinned definition. Changed source
index definitions are checked at worker startup, native page boundaries, and
before each exposed batch or cached physical-layout slice. An identical index
recreated under the same identity is indistinguishable once ready. Previously
fetched source values can remain buffered; this does not freeze current values.

`partition_result_ttl` and `partition_snapshot_id` use the existing server admission
and [DROP PARTITION SNAPSHOT lifecycle](partitioned-reads.md#current-managed-snapshot-contract).
Despite that API name, no application-row snapshot is created. Releasing or
expiring the group prevents new workers; an admitted child reader keeps its own
catalog lease until exhaustion, cancellation or release. Statement/producer close
does not revoke descriptor copies. Reader failure/death leaves at most its bounded
lease; no source-key inventory or persistent SCAN cursor requires cleanup.

Catalog storage format 17 is unchanged. Old clients reject version-5 descriptors;
use a driver containing this implementation for planner and readers. Runtime
checks do not establish every released wheel, backend or deployment combination.
