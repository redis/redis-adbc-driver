# ADBC partitioned reads

This page describes the default `partition_result_mode=live` path. Opt-in
[retained results](retained-partition-results.md) add bounded global SQL,
additional source layouts and immutable replay through the same public APIs.
The [coverage comparison](partition-coverage.md) records remaining gaps and
the source-consistency design boundaries.

`ExecutePartitions` / `ReadPartition` support deferred reads of a single
permanent, driver-managed Redis table. See [issue #163](https://github.com/redis/redis-adbc-driver/issues/163).
This API supplies the split descriptions for an external scheduler such as Ray
or Dask; it does not create workers or provide an installed Ray connector.
Rebuild the native library from this source checkout before using the new API;
these changes have not yet been published in release wheels.

## Supported queries

Use column/star projections, arithmetic, casts, CASE, and these deterministic
scalar functions: `ABS`, `MOD` (also `%`), `LOWER`, `UPPER`, `LENGTH`, `COALESCE`,
`NULLIF`, `GREATEST`, `LEAST`. Predicates use the same row-local expression
allowlist. For example:

```sql
SELECT id + ?, CASE WHEN label IS NULL THEN 'missing' ELSE UPPER(label) END AS label
FROM public.events WHERE id >= ?
```

One bound Arrow parameter row is supported, including BindStream. Empty or
multiple parameter sets fail. The binding is consumed and released during
planning; descriptors own copied typed values, not pointers into the binding.
Types: NULL, bool, signed/unsigned integers fitting int64, floats, decimal
(precision <=76), text, binary, date, time, timestamp (naive or UTC), and
month/day/nanosecond interval, as mapped by the ordinary driver's Arrow type
conversion. Dictionary values map to their underlying type. Arrays, records,
and unsupported Arrow types fail explicitly. Maximum 1024 fields and 1024 input
batches (including empty batches); maximum 256 KiB JSON-encoded parameter section
and 1 MiB total descriptor. Bound timestamps with a non-UTC timezone name are
normalized to UTC as in ordinary execution. NULL keeps its logical type; binary
and string bytes use base64, including embedded NUL/invalid UTF-8. Numeric/temporal
payloads use exact typed storage representations, never SQL interpolation.
Parameter stream failures produce IO errors without including values.

Scripts, subqueries, views, CTEs, joins, aggregates, grouping, DISTINCT, windows,
set operations, ORDER BY, LIMIT and OFFSET remain unsupported. Volatile,
user-dependent and other session-dependent functions remain rejected. Captured
time zone is the only supported session setting used by expressions. Temporary,
adopted, foreign-index and legacy tables without a persistent unique key prefix
are unsupported. Flex remains unsupported. See the
[retained runtime contract](retained-partition-results.md) for global SQL,
additional layouts and stable replay in the opt-in mode.

Broad reads use Search cursors over disjoint numeric subranges. Each subrange spans
at most the guaranteed aggregate cap's number of allocated IDs, so it cannot
match more rows than that cap. Empty/sparse ranges and residual filters are
handled without truncation, page replay, or whole-partition HASH scans. At most
one cursor is open per reader. Ordinary ExecuteQuery and partition reads share
the cursor/Arrow lifecycle and internal range source. Eligible ordinary reads
use that source when a lowered cap requires it, while preserving ordinary
ordering and applying LIMIT/OFFSET once across all ranges. Partition output
remains unordered. Subranges are opened lazily and limited to 2^20
per descriptor; oversized sparse work is rejected explicitly.

`adbc.redis.partition_scan_cap` is an integer/string statement option in
0..2147483648 (default 0). On Search-backed reads, zero requires fresh discovery through CONFIG at read
start and at each subrange, independently of the ordinary query capability
cache. Hidden/denied/nonpositive settings fail explicitly. Named ACL users do
not probe administrative commands; set a positive guaranteed lower bound when
planning for such readers. This is a deployment contract, not a request to raise
a server setting: administrators must ensure the effective cap on every serving
node stays at least that bound during reads. With automatic discovery, keep the
observed bound valid for each open cursor. Lowering it during an active cursor
cannot be made atomic with Redis CONFIG; that operation is unsupported. Fresh
reads/ranges discover subsequent changes; abandoned readers open no new ranges.
Planning/releasing descriptors does not change server configuration or table data.

## Planning and worker execution

The statement option `adbc.redis.partition_count` accepts an integer or string
from 1 to 1024 (default 8). It limits the number of ranges, rather than choosing
concurrency or guaranteeing equally sized partitions. Ranges divide allocated
internal row IDs, not row counts; deletes and sequence gaps can leave empty or
uneven partitions. The row sequence must be below 2^53, where Search numeric
range boundaries are exact. A never-populated table returns one empty partition
with its Arrow schema. A previously populated empty table may return more.

The string statement option `adbc.redis.partition_strategy` defaults to `rowid`
(equal-width allocated-ID ranges). Opt-in `balanced` chooses live-row count
quantiles within the inferred candidate envelope, without estimating residual
selectivity. It performs at most 256 count queries with a two-second internal count-probe deadline; exhaustion discards
partial cuts and retains equal-width ranges. Sparse/empty tables with fewer
live rows than requested partitions also retain the default splits. A candidate
that would exceed the current cap's per-descriptor subrange budget falls back.
Normal Redis metadata/transport timeouts still apply outside the probe deadline.

Counts are advisory workload estimates, not frozen membership. Every candidate
allocated ID remains covered, including currently empty gaps and unfinished inserts below
the captured watermark. Concurrent writes may make counts unbalanced but do not
create overlapping ranges or a snapshot. Balanced planning requires Search
readiness and FT.SEARCH; failures remain explicit. It needs a discoverable cap
or the configured positive contract to protect range width; if automatic cap
information is unavailable it retains the default splits. Independent workers
need no new descriptor fields or scheduler. More planning work can lose on
small/already-balanced queries; use the default unless measurements justify it.

The planner returns the Arrow schema, opaque byte descriptors and an unknown
row count (`-1`). Send the descriptors to workers which open independently
authenticated connections to the same logical database. The planning statement
and connection can close before reads start. Different cluster seed/proxy
addresses are allowed; the table's unique physical identity and schema are
checked on the reading connection. Cloned databases with identical metadata
are indistinguishable and must be routed by the caller.

Cluster API workers also need `ASKING` permission to follow temporary redirects
during slot migration. An ACL that permits steady-state reads can still fail
during resharding without it. The owned
[three-primary TLS check](partition-cluster-tls.md) exercises `ASK` and `MOVED`
using existing native connections and pre-planned descriptors. Search operations
can report temporary topology availability errors or omit a migrated HASH while
their local loads converge, even after slot ownership is final. Cached sortable
IDs and document counts alone do not establish readiness. The fixture waits for
complete HASH loads on every primary before its exact Search-result assertion;
it does not add transparent query or stream retries. Committed retained results
and direct-HASH partitions do not depend on this Search convergence.

Each worker reads Arrow batches using the reading database's
`adbc.redis.stream_batch_rows` (default 65536). Partition reads always stream,
even if ordinary query streaming is disabled. Client buffering is bounded by
the Arrow batch setting plus Search cursor or HASH pipeline pages; it does not grow with the
entire result. Output order is unspecified. Do not concatenate partitions and
assume a global sort.

## Standard incremental execution

`adbc.statement.exec.incremental` accepts canonical strings `true`/`false`,
default `false`. It follows the [canonical ADBC header](https://arrow.apache.org/adbc/main/cpp/api/adbc_8h_source.html):
all deferred ranges are available together, so the first ExecutePartitions call
returns them all. Subsequent calls return an empty partition set, the same
schema and unknown row count (-1). Empty means terminal completion, never
“temporarily unavailable.” An empty table still emits an empty-read descriptor
first, then terminal completion. No worker/background task is started or polled;
metadata/network planning and bound stream consumption still take their normal
time. This option does not make arbitrary SQL evaluation asynchronous.

A discovery failure is terminal and repeated calls return that error until
reset. SetSqlQuery, Bind/BindStream, setting the incremental option (including
the same value), partition count/cap/strategy or ingest target resets discovery. Closing
the statement clears its schema/error state; closing its connection makes further
calls fail. ExecuteQuery/ExecuteSchema are unaffected. In-flight execution uses
the existing context/C API cancellation path; once descriptors have been returned,
there is no pending discovery work to cancel. Statement cancel then reports no
active query unless it owns another streamed result. Reset before trying again
after cancellation. Returned descriptors survive planner abandonment/close and
are not revoked by resets or option changes. Readers belong to worker connections.

The standard double progress/max-progress options explicitly return
NOT_IMPLEMENTED: metadata discovery has no useful intermediate execution progress.
Consumer rows read are not query execution progress. No universal partition or
incremental capability flag is invented in GetInfo.

Finite Arrow batch streaming fetches cursor pages as a reader advances.
Partitioned reads distribute deferred read descriptions. Incremental execution
controls repeated discovery of those descriptions. None of these is a continuous
subscription, change feed, CDC stream or shared transactional snapshot.

## Selective internal row IDs

Planning extracts exact integer `__rowid` equality, IN, BETWEEN and inequalities
(in either operand order), including integer typed bindings and unary signs.
It intersects bounds with the captured allocated-ID domain. AND can retain a
safe conjunct; OR requires every branch to have a safe constraint. Float,
decimal, text, NULL, casts, functions, NOT and mixed OR expressions retain the
conservative range path. Business-column IDs never imply internal row IDs.
Full residual predicates are rechecked, including SQL NULL semantics.

At most 1024 proven candidate IDs are enumerated. Their groups emit at most
`partition_count` descriptors; an empty set emits one empty descriptor with its
schema. Larger interval unions use a conservative envelope, including gaps,
with the usual rowid/balanced splits. Extraction is bounded to 4096 nodes,
128 nesting levels and 1024 intervals; budget exhaustion widens candidates.
The table's below-2^53 restriction and descriptor/discovery format still apply.

A worker whose descriptor proves at most 1024 candidate IDs reads authoritative
HASHes in pipelines of at most 1000 keys. It shares Arrow batching, cancellation,
connection ownership and exact decoding with the streamed reader; it checks
physical identity before and after fetching. Missing/deleted keys produce no
rows. Updates remain live; IDs above the captured bounds remain excluded.

These Search-free reads need connection/metadata and HASH read permissions,
but do not issue CONFIG, FT.INFO, FT.SEARCH, FT.AGGREGATE or FT.CURSOR for their
input. No scan-cap guarantee or index-readiness permission is needed on this
path. Invalid descriptor cap values still fail validation. Other predicates
retain all Search-backed cap/readiness checks below. Direct HASH lookup does
not grant extra permissions or introduce a snapshot. Ordinary ExecuteQuery
also retains normal numeric predicate evaluation for non-integer row-ID
comparisons instead of rounding them into an equality key.

## Search index readiness

Redis OSS 8.6.2/Search 8.6.0 emitted duplicate cursor rows while an index
was still building. Bounded Search readers now check `FT.INFO` before and after
opening each cursor and fetching each page. OSS Cluster readers check every
primary; Software/proxy endpoints must expose the readiness fields through
`FT.INFO`. A building/incomplete index fails with `INVALID_STATE`: wait for
indexing to finish and retry the descriptor. Unsupported/missing readiness
fields fail with `NOT_IMPLEMENTED`. Failed pages are discarded and their
cursors are closed; the driver does not deduplicate or replay emitted pages.

Search-backed partition workers additionally need `+ft.info` and permission to inspect their
index. A guaranteed positive scan cap does not bypass this check; CONFIG remains
optional with that contract. A denied readiness check fails with `UNAUTHORIZED`.
Eligible ordinary queries retain their complete HASH fallback when readiness
or its permission cannot be established before selecting bounded scans. If
readiness changes after a bounded read starts, that read fails explicitly.

A SQL COUNT alone does not establish Search readiness. Deployment owners can
also verify `indexing=0` and expected document counts before distributing work.
The checks mitigate the observed building-index condition, not every possible
backend defect or concurrent external index replacement. They do not create a
snapshot or guarantee stable retries. See
[issue #175](https://github.com/redis/redis-adbc-driver/issues/175) and the
[original evidence](../benchmarks/partitions/evidence-2026-10-21/README.md).

Indexed ALTER operations wait for Search backfill before publishing their new
metadata. Background dropped-field cleanup remains asynchronous. Search-backed
partition reads reject a table with pending cleanup using `INVALID_STATE`;
retry the descriptor after an authorized cleanup worker completes the work.
Proven direct HASH reads do not depend on that index state and remain available.
Balanced planning retains equal-width cuts during cleanup. Ordinary scans and
aggregates use complete HASH inputs during that interval, with their normal
global operators. No broader reader ACL or shared snapshot is introduced.

## Consistency, lifecycle and permissions

Descriptors describe future reads, not retained results. There is no expiry
timer, coordinator lease, temporary result table or server-side plan state to
clean up. Releasing the C partitions structure releases its byte storage only;
copies already sent to workers remain usable. Closing or reusing the planning
statement does not revoke them. Table drop/recreation, truncate, rekey or
physical schema changes invalidate descriptors; plan again after those changes.

Planning captures the allocated row-ID high watermark. Inserts allocated after
that point are excluded. An insert already allocated but not finished may
appear later. Updates and deletes are observed when each worker reads, and
re-reading a descriptor may return different values. There is no shared
transactional snapshot, stable retry result, or exactly-once guarantee. On an
unchanging dataset, ranges cover the query's rows without overlaps or omissions.

Release/close each Arrow reader promptly: this deletes its Search cursor.
Connection close, cancellation of the Go read context, and the C/Python
connection cancellation API stop its readers. Python's cursor statement cancel
does not own a partition read: use `conn.adbc_cancel()`. Redis also expires idle
cursors. A failure can occur during batch iteration, so consume and check the
reader before treating a partition as successful.

Descriptors contain SQL text/literals, typed bound values, schema and table names, time zone and
physical identity hashes; treat them as potentially sensitive query data.
They contain no URI, password or live pointer. They are validated and reparsed
as untrusted input, are not signed, and are not authorization capabilities.
Readers use their own Redis ACL permissions; changing descriptor bytes cannot
grant permissions. Malformed/version/size/range errors fail with
`INVALID_ARGUMENT`; stale/wrong-identity targets fail with `INVALID_STATE`.
Missing objects and denied commands retain ordinary driver errors. Validation
does not echo descriptor contents.

The planner captures the resolved schema and time zone, so workers do not
depend on the planner's session or inherit its credentials. Other session
dependent expressions are excluded by the query subset above.

## Python example

Install `adbc-driver-redis[dbapi]` and PyArrow on each worker, and configure a
supported Redis deployment with Search. With an existing `public.events` table:

```python
from adbc_driver_redis import dbapi

uri = "redis://localhost:6379/0"  # worker-accessible endpoint
with dbapi.connect(uri) as conn:
    with conn.cursor() as cur:
        cur.adbc_statement.set_options(**{"adbc.redis.partition_count": 4})
        partitions, schema = cur.adbc_execute_partitions(
            "SELECT id, label FROM public.events WHERE id >= 100"
        )

# Run independently on each worker for its assigned descriptor.
def read_partition(descriptor):
    with dbapi.connect(uri) as conn:
        with conn.cursor() as cur:
            cur.adbc_read_partition(descriptor)
            with cur.fetch_record_batch() as batches:
                for batch in batches:
                    consume(batch)  # your processing code
```

For a runnable local process-pool example, see
[adbc_partitions.py](../python/examples/adbc_partitions.py). It counts rows
incrementally and compares Arrow schemas on each worker; it creates no tables.

## Follow-up scope

The capabilities beyond this initial implementation are tracked separately:

- [#165: Bound parameters and deterministic row expressions](https://github.com/redis/redis-adbc-driver/issues/165) — implemented.
- [#166: Global operations and multi-relation SQL](https://github.com/redis/redis-adbc-driver/issues/166).
- [#167: Additional table layouts and larger row IDs](https://github.com/redis/redis-adbc-driver/issues/167).
- [#168: Lowered or undiscoverable Search aggregate caps](https://github.com/redis/redis-adbc-driver/issues/168) — implemented with the documented cap contract.
- [#169: Stable snapshots and replayable results](https://github.com/redis/redis-adbc-driver/issues/169).
- [#170: Compatibility CI and performance evidence](https://github.com/redis/redis-adbc-driver/issues/170).
- [#173: Incremental partition discovery](https://github.com/redis/redis-adbc-driver/issues/173) — implemented with eager terminal discovery.

## TLS and independent-worker validation

`scripts/start_partition_tls.sh` creates a disposable, TLS-only Redis 8.6.2
fixture with a fresh local CA and real aggregate cap 137. It accepts a new
fixture directory, container name and localhost port. Set
`SSL_CERT_FILE` to its `ca.crt` and `REDIS_TLS_ADMIN_URI` to the contents of
`admin.uri`, then run `scripts/check_partition_tls.py --output <fresh JSON>`.
Use `REDIS_ADBC_LIBRARY` for a built library, or install the native wheel and
omit that override; `--require-wheel` verifies the installed-package path.
The Python environment needs PyArrow, adbc-driver-manager and redis. The checker
passes that CA using the database option `adbc.redis.tls_ca_file`, including to
each fresh worker, rather than relying on inherited CA environment variables.

For application connections, set `adbc.redis.tls_ca_file` to a local PEM trust
bundle when using a private CA. Nonempty replaces default system roots for new
connections; empty uses system trust. It requires verified `rediss://` and cannot
be combined with `skip_verify=true`. Each worker supplies its own path; the file
is not included in descriptors. Existing connections do not reload rotated files.
Publicly trusted endpoints need no custom CA option.

The checker creates its own schema and distinct planner/read-only worker
principals, closes the planner before starting fresh worker processes, and
validates complete values and schemas. It checks untrusted certificate and
hostname rejection, denied CONFIG/writes/HASH/readiness commands, cancellation,
early release, live updates/deletes, later-insert exclusion and stale targets.
Generated credentials, URI files and certificate keys must stay local; only the
credential-free result JSON is a CI artifact. The checker removes its generated
schema/users. Remove the fixture container and directory after use; it does not
install a CA into the system trust store or change an existing Redis service.

PR tests require the TLS check on both Linux OSS-single architectures, reusing
the built library. Release validation uses installed Linux and macOS wheels.
The macOS job starts an owned native TLS-only Redis and checks default/minimum
ADBC clients with 15 TLS checks, including native CA and hostname rejection.
Local macOS packaged validation passed on 2026-10-21; hosted results are separate
evidence. Cloud/proxy TLS and remaining compatibility gaps stay tracked in [#170](https://github.com/redis/redis-adbc-driver/issues/170).


Redis Software proxy TLS uses `scripts/software_partition_tls.py` after the
existing disposable Software setup. It verifies ownership of the Compose
container and localhost ports, installs private API/proxy certificates, requires
TLS/password authentication and verifies the real cap 137 and active primary
count. Initial local management bootstrap uses the fixture's self-signed API;
subsequent management and all native partition connections verify the private CA.
Use `--software-fixture DIRECTORY` with the same checker to provision database-only
roles/users through the management API. Software's unauthorized `CONFIG GET`
returns an empty response; the checker verifies that policy and still requires
`CONFIG SET`, writes, denied readiness and HASH reads to fail.

Both Linux PR architectures require Software single/three-shard checks after
non-TLS suites; Linux release jobs also validate the installed wheel through a
single-shard TLS proxy. Local macOS single/three-shard and Linux arm64 packaged
runs passed all 16 Software checks. Each three-shard deployment uses one Software
node, so this establishes proxy/shard routing coverage, not multi-node failover.
See [verification](sdlc/changes/software-proxy-tls-400/plan.md). Cloud TLS and
remaining performance evidence are still open in [#170](https://github.com/redis/redis-adbc-driver/issues/170).
