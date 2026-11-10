# Connection-scoped shared read snapshots

Native ADBC can explicitly bind one managed catalog generation to a connection
for sequential full-refresh reads. This is a read-only API, not a multi-statement
write transaction. It extends [statement snapshots](snapshots.md).

**Release status:** unreleased; v0.0.23 does not expose these options. Do not enable
Airbyte `read_consistency=shared_snapshot` against v0.0.23. A release containing
this implementation and its validation must be selected and pinned before that
connector setting can be enabled. Source validation is recorded in the
[change evidence](sdlc/changes/shared-read-snapshot/verification.md).

## Native connection API

| Connection option | Contract |
|---|---|
| `adbc.redis.read_snapshot_retention_ms` | String or ADBC int64 milliseconds, 120000–3600000, default 900000. Set before acquire. Fixed maximum lifetime, including active readers; cannot be extended. |
| `adbc.redis.read_snapshot_action=acquire` | Acquire and immediately bind a committed generation. Refuse an existing binding, active readers, temporary objects or a retained-reader-only connection. Cancellation during acquisition fails it. The successful binding outlives the option call's context. |
| `adbc.redis.read_snapshot_status` | Read-only JSON: `state`, `generation`, `deadline` (UTC). Unbound: `{"state":"unbound"}`. An unavailable bound view returns an error. The generation is diagnostic, not an attachment capability. |
| `adbc.redis.read_snapshot_action=release` | Idempotently unbind. Active readers retain their original generation until close/exhaustion/cancellation or the original deadline. Subsequent new statements use ordinary statement snapshots. |

Only the acquiring connection can use the binding. There is no exported handle,
attachment, cross-process API or snapshot-enabled partition descriptor.
`ExecutePartitions` and `ReadPartition` refuse while bound. `attach` and all
unknown action values fail explicitly. Acquire on another connection establishes
an independent view and must never be described as joining this one.

While bound, GetObjects callbacks, GetTableSchema, schema selection, parameter
schema discovery, ExecuteSchema and supported read-only SQL SELECTs use the same
generation. GetObjects retains one view for its complete discovery call and
validates it even for catalog-only depth or filters matching no catalog.
Managed tables, imported managed copies and SQL view definitions are
versioned together. Statement-local caches are rebuilt against that generation.
DML, DDL, grants, retained operation actions, ingestion, sequence allocation
(`nextval`/`setval`), temporary objects, Substrait, IPC and CUDA execution are
unsupported on the bound connection. Use another connection for publications.
Volatile expressions (time, random values and session settings), sequence counter
inspection and live server/system information are not frozen catalog data.

## Boundary, authority and bounds

The boundary is the existing managed catalog and its storage protocol 17 in one
Redis hash slot. Supported DML/DDL, TRUNCATE, DROP and compaction preserve pinned
row versions and metadata. This provides neither coordinated snapshots over
independently distributed shards (#340) nor upstream native Redis read views
(#339). It adds no persisted format or upgrade path: fresh-storage requirements
and rejection of earlier protocols remain unchanged.

Imported copies retain exactly the captured managed values and hidden source-key
provenance. Their earlier application HASH/index capture remains weaker and is
not made atomic by this binding. Later external source TTL expiry does not expire
the managed copy. Direct mutation, eviction or expiry of driver-owned metadata,
rows, directories or indexes is outside the managed protocol. Existing complete
row/index/directory proofs and read fences must fail on unavailable data; no
successful partial extraction or fallback to live storage is allowed. Use
noeviction and the documented ownership/ACL policies.

The view uses its connection's authenticated Redis client, catalog and reader
ACL domain. Every command remains subject to current Redis command/key
permissions and stable logical SQL grants across physical generations. A retained
view does not retain a revoked SELECT grant; subsequent data access fails after
Redis Software propagates revocation. As in [SQL RBAC](sql-rbac.md), REVOKE
is not a synchronous barrier for in-flight queries, open cursors or already
fetched/buffered results; it cannot retract copies already read by a client.
Metadata retains the existing metadata
visibility policy; it does not grant row access. No token or credential is
exported. Authorization cannot transfer to another principal, catalog or tenant.

The connection and live readers locally reference one existing server pin.
Closing one consumer releases only its reference. Connection Close cancels its
readers, releases the binding, and then closes its clients. Release without
closing readers allows those readers to finish under the original view. Detached,
fully materialized Arrow results hold no server pin. Consumer cancellation does
not silently unbind the connection; release it in a finally block when abandoning
a sync.

The existing two-minute server lease renews automatically every 30 seconds, only
until the configured fixed lifetime. There is no manual renewal or extension API.
Expiry is checked before handing out buffered streamed batches. Process death
stops renewal; after at most two minutes from the last acknowledged renewal,
normal acquisition/sweep can prune the pin and collect retired generations.
Collectors run on subsequent activity; an idle server does not promise immediate
physical deletion. Failed cleanup is bounded and uses the existing idempotent
release retry/expiry mechanism.

Existing limits remain 128 live catalog pins and 16 catalog generations, with
existing metadata/control byte budgets, row/stream budgets and Redis capacity
limits. Retaining an old root can backpressure publishers; limits return explicit
errors rather than dropping old data or truncating results. There is no new
unbounded retention domain. Individual SELECT timeouts remain in force even when
the connection lifetime is longer.

Expiry, lease loss, unavailable authority or incompatible storage fails the
requested view. It is never replaced with a newer generation. A failed validity
check is terminal for that binding (caller cancellation alone does not poison
it). Retry a statement on the same connection only while that view remains
valid and authorized. Otherwise release it, discard the failed full-refresh
attempt, acquire a new view and redo discovery and all table reads. Already
emitted batches are provisional until the whole attempt succeeds. This API
provides no durable Airbyte checkpoints or exactly-once destination effects.

## Python ADBC example

Acquire before discovery and drain each result on the same dedicated connection.
Use an installed native library containing the API; this example expects existing
`orders` and `order_items` managed tables.

```python
import adbc_driver_manager.dbapi

with adbc_driver_manager.dbapi.connect(
    driver="/path/to/libadbc_driver_redis.so",
    db_kwargs={"uri": "rediss://user:password@host:port/0"},
    autocommit=True,
) as conn:
    options = conn.adbc_connection
    options.set_options(**{"adbc.redis.read_snapshot_retention_ms": 900000})
    options.set_options(**{"adbc.redis.read_snapshot_action": "acquire"})
    try:
        with conn.adbc_get_objects() as metadata:
            objects = metadata.read_all()
        orders_schema = conn.adbc_get_table_schema("orders")
        items_schema = conn.adbc_get_table_schema("order_items")
        with conn.cursor() as cur:
            cur.execute("SELECT * FROM orders")
            orders = cur.fetch_arrow_table()
        with conn.cursor() as cur:
            cur.execute("SELECT * FROM order_items")
            order_items = cur.fetch_arrow_table()
    finally:
        options.set_options(**{"adbc.redis.read_snapshot_action": "release"})
```

Use `.dylib` or `.dll` as appropriate. The executable native-boundary regression
is [shared_snapshot_validation.py](../go/validation/shared_snapshot_validation.py).

## Airbyte and Flight SQL

Native connector integration must feature-detect successful acquisition, pin a
released driver version containing this API, bind **before** discovery/schema
validation, and use that connection for all selected managed tables/views.
Disable parallel connections and partition workers for this mode. Drain/close
all Arrow readers and release in success, failure and cancellation paths. Treat
acquire/metadata/read failures as a failed full-refresh attempt, never as
permission to downgrade to per-statement consistency.

The current connector at
[`46b4c3c6`](https://github.com/Redislabs-Solution-Architects/redis-airbyte-connector/tree/46b4c3c6d8286f017293518630028c48ed256eb8)
still rejects this setting for both transports. A driver release alone does not
remove that rejection. For its native integration:

1. In `SourceRedisAdbc._backend`, replace the blanket shared-snapshot rejection
   with a native-only capability path; continue rejecting Flight SQL. Keep
   configuration and sanitized error handling in place.
2. In `AdbcBackend.connection`, acquire immediately after opening the native
   connection and before yielding it to `check`, `discover` or `read`. Set the
   lifetime and action through `connection.adbc_connection.set_options`; these
   are connection options and must not be passed as `database_options`.
3. Keep the existing `read` rediscovery and validation of **all** selected
   schemas before its first record. A catalog returned by a previous Airbyte
   discover request is not the read attempt's shared view. Run all table queries
   and consume their complete batches inside that connection's binding.
4. Release in the connection context manager's finally block after readers close,
   including generator close/cancellation. Keep partial-emission failure visible
   and require a new full-refresh attempt after expiry/loss. Never retry on a
   fresh connection within the same attempt.
5. Update the connector's bundled native library or explicit `REDIS_ADBC_LIBRARY`
   deployment to the selected released artifact. Its `driver_path` resolves its
   own bundled library/override; merely installing a newer `adbc-driver-redis`
   package does not change that path. Verify the loaded version and feature-probe
   acquisition before claiming support.
6. Add connector tests for two selected tables and pinned metadata while a second
   client publishes DML/DDL, exact schema/value/multiplicity comparisons, and
   failed acquire/read/expiry/cancellation without fallback. Verify Flight SQL
   rejection independently. Gate enabling `read_consistency=shared_snapshot` on
   these tests and the pinned released driver version.

The inspected integration points are
[`source.py`](https://github.com/Redislabs-Solution-Architects/redis-airbyte-connector/blob/46b4c3c6d8286f017293518630028c48ed256eb8/source_redis_adbc/source.py)
and
[`backend.py`](https://github.com/Redislabs-Solution-Architects/redis-airbyte-connector/blob/46b4c3c6d8286f017293518630028c48ed256eb8/source_redis_adbc/backend.py).
These are required integration steps, not evidence that the external connector
has implemented or released them.

Flight SQL shared snapshots are unsupported. No gateway metadata-session binding,
connection option propagation or gateway ownership/lease protocol is implemented
or validated here. A Flight SQL connector must explicitly reject
`read_consistency=shared_snapshot` until its own transport implementation and
independent evidence exist. Native results do not establish gateway support.
