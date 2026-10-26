# Retained partition results

Retained mode extends `ExecutePartitions` with global SQL and immutable Arrow
replay. It runs a bounded coordinator query once, stores its completed final
output, and returns version-2 descriptors over contiguous chunk ordinals.
`ExecuteQuery` remains the ordinary SQL API. Live row-range partitions remain the
default and are useful when workers should read current source rows without
storing a result. This implementation addresses the runtime work in [#166](https://github.com/redis/redis-adbc-driver/issues/166), [#167](https://github.com/redis/redis-adbc-driver/issues/167)
and [#169](https://github.com/redis/redis-adbc-driver/issues/169); the deployment/performance matrix in [#170](https://github.com/redis/redis-adbc-driver/issues/170) is tracked separately.

## Configure and use

Set database option `adbc.redis.partition_result_namespace` to a tenant-owned
name of 1–64 ASCII letters/digits/underscores/hyphens. Set statement option
`adbc.redis.partition_result_mode=retained`, then call `ExecutePartitions` with
one SELECT of at most 32 KiB. Existing typed single-row parameter bindings,
`adbc.redis.partition_count`, `adbc.redis.stream_batch_rows` and the optional
`adbc.redis.stream_batch_bytes` apply. The byte setting uses the ordinary stream's
logical accounting and allows one oversized row to make progress; zero disables
it. A planner statement limits output chunks; a worker database independently
limits returned Arrow slices. A worker still holds one bounded IPC chunk, so the
byte setting is not a resident memory ceiling. Very small planner batches can
exhaust the configured chunk count. Partition
count is an upper bound on contiguous output chunk groups, not source workers.
The ordinary executor runs with pushdown disabled and one query worker on
captured source rows; the output may contain fewer partitions than requested.
An empty output still has one schema-bearing empty chunk/descriptor.

Independent consumers configure the same namespace and database option
`adbc.redis.partition_reader_only=true`, authenticate independently, then call
`ReadPartition`. This bootstrap performs no source catalog recovery, namespace
initialization or SQL/ingestion. Set `adbc.redis.cluster=true` for a restricted
Cluster API credential so setup need not probe INFO. TLS verification and
`adbc.redis.tls_ca_file` apply to source, publisher and worker connections. RESP3
is required; RESP2 and `protocol=2` are unsupported.

For ordered output, concatenate descriptors in the order returned, and batches
within each descriptor in read order. Parallel arrival order is not global SQL
order. Each descriptor is independently retryable; retries reproduce its stored
values and order. Unspecified sort ties and aggregate input ordering are not a
cross-execution ordering guarantee. Supply a total ORDER BY, including an
aggregate's own ORDER BY where needed, for deterministic fresh executions.

## SQL and source coverage

| Operator | Retained coordinator behavior | Resource/lifecycle boundary |
|---|---|---|
| Projection, filters, casts, CASE, typed bindings | Ordinary SQL evaluator and Arrow schema | Bounded types, scalar size and expressions |
| ORDER BY, LIMIT/OFFSET | One global sort and final slice | Sort keys/indices admitted before allocation |
| DISTINCT / DISTINCT ON | One global reduction | Cumulative expression and intermediate credits |
| Aggregates, GROUP BY, HAVING | Reduce once, including NULLs and duplicates | Captured input and work/value credits |
| Windows and QUALIFY | Evaluate once over global input | Cache admission; conservative allowance for aggregate frames |
| UNION/ALL, INTERSECT, EXCEPT | Ordinary set/multiset semantics | Both input branches share job limits |
| Joins | Shared captured dependencies; ordinary join semantics | Candidate arrays/maps admitted before allocation; work cap |
| CTEs, permanent views, subqueries | One coordinator session and dependency cache | Definition checks; shared budgets and deadline |

The initial stable-function allowlist includes ordinary aggregates, basic math,
string case/length/trim/concatenation, conditional functions, session reads,
positional string slicing (`SUBSTR`, `LEFT`, `RIGHT`),
ranking/value windows, arrays and current time. Expanding generators, table
functions, recursive CTEs, lateral items, volatile/side-effect functions,
regex/JSON generators and catalog/OID aliases are rejected explicitly. Trim
cutsets must be bounded literals; ROUND/TRUNC precision must be a literal in
[-76,76]. An unsupported plan fails before returning descriptors.

| Source layout | Coverage and ownership |
|---|---|
| Permanent managed table | Exact authoritative HASH reads; signed int64 physical IDs, including values above 2^53 through MaxInt64; no floating Search range boundaries |
| Adopted key-identified HASHes | Exact byte-key deduplication within the adopted row scope; source HASHes remain application-owned |
| Application / foreign HASH Search index | Exact keys across declared prefixes, deduplicated; per-key membership respects index FILTER; reads only declared schema fields |
| Legacy table metadata without a unique physical prefix | Captures existing physical keys; no retained-planning migration/rekey |
| Producer-owned temporary tables/views | Capture before publication; readers replay committed output after producer closure |
| Historical adopted non-key layouts or unowned temporary relations | Explicitly unsupported |

Capture scans all relevant source rows/columns before evaluating predicates.
It does not yet push projections/predicates into capture. Duplicate projected
rows remain duplicates. For non-UTF-8 application keys project
`CAST(__key AS BYTEA)`; invalid UTF-8 typed TEXT output is rejected, not repaired.
Managed input is sorted by exact physical ID for ordinary insertion-order
behavior; arbitrary application keys have no implicit SQL order.

Capture does not rewrite/adopt application data, delete external indexes or run
source catalog housekeeping. Normal publisher database Open retains its existing
SQL catalog migration/recovery behavior; retained mode does not redefine that
older bootstrap contract. Reader-only Open bypasses it.

Source schema/index identity, permanent view definitions, managed/foreign
cardinality and Cluster topology are checked before publication. Active Cluster
slot migrations and observed routing changes fail planning; retry after routing
converges. Cluster source publishers therefore need CLUSTER NODES as well as
normal topology discovery. Keep source DDL quiescent: before/after identity
checks cannot detect a definition changed and restored between checks.

The frozen object is the **completed query output**, not an atomic Redis source
snapshot. Concurrent same-cardinality DML can yield mixed-time input. Persistent
membership/definition/routing changes are rejected where detectable. Readers
never rerun SQL or consult source tables after commit. Stable replay does not
give exactly-once effects in a downstream sink.

See the [remaining coverage and snapshot assessment](partition-coverage.md) for
the comparison with live/ordinary SQL and dependencies for future source consistency.

## Limits and persistence

Limits are cumulative logical allocation/work credits, not process RSS or Redis
allocator/replication/index-memory guarantees. Credits are not refunded within a
query. Defaults can reject wide inputs well below the row limit. Resource
exhaustion returns an error (SQLSTATE 53200), never truncated successful output.
There is no disk spill. Concurrent coordinators each consume their execution
allowance; output namespace quotas do not constitute a process-wide memory cap.

Statement `adbc.redis.partition_execution_limits` accepts a partial JSON object:

| Field | Default | Hard maximum |
|---|---:|---:|
| transport_bytes | 134217728 (128 MiB) | 1 GiB |
| source_bytes | 67108864 (64 MiB) | 1 GiB |
| working_bytes | 268435456 (256 MiB) | 1 GiB |
| work | 1000000 | 10000000 |
| source_rows | 100000 | 1048576 |
| candidate_keys | 200000 | 2000000 |

Dedicated source pools validate RESP headers before go-redis allocates decoded
collections/bulk payloads: 1 MiB bulk, 4,096 collection entries, depth 32 and
4,096-byte headers. All connections/nodes share the transport credit. Known-field
HASH and membership reads use bounded pipelines. The coordinator has a one-minute
execution deadline; cancellation closes its source sockets. Scalar values are
limited to 64 KiB, schema columns to 1,024, nested type depth to 32, expression
nodes to 4,096, and decimal precision to 76. Arrow chunks use the restricted
validated format in [the storage contract](retained-partition-storage.md).

Database `adbc.redis.partition_result_limits` accepts partial JSON fields
`job_bytes`, `total_bytes`, `chunk_bytes`, `chunks`, `jobs`. Defaults are
64 MiB/job, 256 MiB/namespace, 1 MiB/chunk, 1,024 chunks and 1,024 jobs/tombstones.
Hard maxima are 1 GiB/job, 8 GiB/namespace, 16 MiB/chunk and 1,024 chunks/jobs.
All publishers in a namespace must agree on the persisted policy. Reserve the
job output allowance before capture; return unused bytes on commit. All tenant
keys share one Cluster slot, concentrating retained traffic on one primary.

Quota control keys must survive payload eviction. The default
`adbc.redis.partition_control_persistence=auto` checks CONFIG GET
maxmemory-policy on every primary, before upload and publication. It accepts
noeviction and volatile eviction policies, rejects allkeys/unknown policies.
Volatile payload eviction is allowed but fails affected replays. If the provider
restricts CONFIG, an operator may set `guaranteed` only after independently
verifying persistent control keys are protected. Preserve that condition for the
namespace lifetime; this option is an operator assertion, not a driver check.

## Lifetime, failures and authorization

Statement `adbc.redis.partition_result_ttl` is integer seconds, default 300,
range 1–86400, measured from publication with no read renewal. Uncommitted
uploads have a fixed ten-minute lease. A dead coordinator before publication
leaves no readable manifest; its bounded reservation expires and is pruned on
the next quota operation. Cancellation/error attempts nonce-authorized cleanup
using a fresh deadline-aware pool. Transport-uncertain operations are not retried
as a new SQL execution.

Closing producer handles preserves committed results. Early consumer release,
worker cancellation/death and partial/full replay do not revoke shared output.
Readers fetch one chunk at a time and check state/checksums/schema/counts before
decoding. Already downloaded values can remain consumable until the next chunk;
revocation is not a recall of local Arrow memory. Replay socket waits are bounded
to at most 30 seconds. Missing/evicted/corrupt/expired/revoked output fails with an
error and never silently falls back to source execution.

A namespace-authorized publisher can execute:

```sql
DROP PARTITION RESULT '0123456789abcdef0123456789abcdef';
```

Use the job ID from the descriptor. Revocation atomically deletes the manifest
and complete bounded chunk key space and returns byte quota. A zero-byte
tombstone counts against the job limit until its original expiry. Explicit
revocation repairs missing/corrupt job metadata using the authoritative expiry
ledger. It cannot repair destroyed quota ledgers; control corruption fails
closed. Cleanup never deletes source data. Small namespace control keys persist
until operator cleanup after all jobs expire.

Descriptors contain no credentials, endpoint, SQL or arbitrary Redis keys and
are not bearer authorization. A minimal worker needs `+ping +time +getrange`
and `%R~adbc:{retained:TENANT}:*`; Cluster additionally needs `+cluster|slots
+command +asking`. Publishers need existing SQL/source privileges, source SCAN,
known-field reads and index membership, topology checks when clustered, plus
namespace EVAL and the scripts' declared String/ZSET operations. Publishers are
trusted within their namespace. Give separate tenant credentials/namespace
policies; key ACLs remain the authorization boundary.

Retained mode adds a complete coordinator read, serialization/upload, stored
output until expiry and one download per worker/retry. It is useful for global
SQL and stable retries, not an inherent speedup over ExecuteQuery/live scans.
See [implementation evidence](sdlc/changes/retained-execution-budget/evidence.md)
and [deployment validation](sdlc/changes/retained-deployment-validation/evidence.md)
for tested revisions, compatibility coverage and scoped measurements.
