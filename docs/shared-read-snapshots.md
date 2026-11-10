# Reusing one managed read snapshot

The native Redis ADBC driver can bind a connection to one committed catalog
before discovery and reuse it for a sequential full-refresh extraction. This
capability is introduced by [issue #365](https://github.com/redis/redis-adbc-driver/issues/365).
It is not present in v0.0.23. A release containing this implementation must be
published and validated before a connector enables `read_consistency=shared_snapshot`.
No released version is claimed by this document.

## Native ADBC API

These are string **connection** options, available through ADBC SetOption/GetOption:

| Option | SetOption | GetOption |
|---|---|---|
| `adbc.redis.read_snapshot` | `acquire` captures and binds the current generation; `release` unbinds it | `acquired` while valid; empty before acquire/after release; error after loss/expiry |
| `adbc.redis.read_snapshot_id` | Read-only; setting an ID rejects attachment | Generation identity for diagnostics; empty when unbound; error when invalid |
| `adbc.redis.read_snapshot_retention_ms` | Integer 1000–86400000, set while unbound | Configured retention; default 3600000 (one hour) |

Acquire must succeed **before** GetObjects, GetTableSchema, schema validation or
any table SELECT. Acquiring again while bound fails, including after expiry.
There is no exportable capability or implicit bind on a second connection. The
ID cannot be used for attachment. Use one authenticated connection for discovery
and all reads; connection pools must retain that exact connection. Connection
options are not safe to change concurrently with initiating other operations.
Use another connection for writers.

Supported operations are read-only SQL SELECTs (including joins, managed views,
CTEs and complete streamed Arrow consumption), GetObjects, GetTableSchema,
ExecuteSchema, GetParameterSchema and GetStatistics. Substrait binding/execution
uses the same view. Session schema selection may select a schema in that view.
Temporary objects are excluded: acquisition refuses a connection with temporary
objects, and creation is forbidden while bound. Writes, data-modifying CTEs,
sequence functions, SQL session/utility statements, Arrow ingestion, retained
operations, foreign refresh, ExecutePartitions and ReadPartition are rejected.
No worker descriptors are produced. Partition-reader-only connections cannot
acquire this view. There is no cross-connection or cross-process attachment.

## Consistency and authorization

The same immutable catalog root supplies schema names, table schemas, view
definitions and managed row versions throughout the view. Other clients can
publish DML/DDL, TRUNCATE, DROP or storage compaction without changing it. Fresh
ordinary connections see subsequent publications. Exact values, NULLs, duplicate
multiplicities and imported hidden provenance retain their statement-snapshot
meaning. The boundary is the existing protocol-17 metadata slot and managed
storage, described in [statement snapshots](snapshots.md). This adds no
coordinated snapshot over independently distributed shards (#340) and no native
Redis read-view primitive (#339). It adds no write transaction or rollback.

Authentication and native Redis ACLs are those of the owning connection, including
SQL SELECT grants translated into native ACLs. A generation ID grants no access.
Each operation constructs fresh metadata/segment validation state; row and Search
commands remain subject to current native permissions. Revocation can fail an
ongoing read; this view does not freeze or bypass permissions. Metadata retains
its existing discovery permissions, separate from row SELECT grants. Follow
[SQL RBAC](sql-rbac.md), TLS and tenant isolation requirements; do not distribute
IDs as credentials.

An already published managed import is immutable within this view. Its earlier
application HASH/index capture still has the documented weaker consistency.
External source TTL expiry does not expire the independent managed copy.
Out-of-protocol writes, expiration or eviction of managed storage are unsupported;
missing/damaged rows or indexes must produce the existing snapshot validation
errors, never a successful partial extraction. Use noeviction and ACL isolation.

## Ownership, retention and failure

Acquisition owns one normal server reader pin. It renews every 30 seconds under
a two-minute lease, independently of individual statement cancellation, until
release, connection close, lease loss or the fixed retention deadline. Renewal
never extends the retention deadline. Statement execution/streams retain the
existing 15-minute SQL deadline (Substrait retains its existing shorter bound).
Command timeouts and cancellation still apply. One bound view consumes one of
the catalog's 128 reader pins and retains its generation within the existing
16-generation admission bound. Older data can consume storage and cause writer
backpressure or quota refusal. Bounds fail explicitly; they never truncate a
successful result or silently choose a newer generation.

Children borrow the connection-local parent pin. Exhausting a result, closing a
reader early, canceling a statement or encountering a query failure retires only
that child. The application must release in `finally` on success **and failure**.
Release cancels active Redis-backed readers; they must fail rather than complete
under another generation. Already fully materialized Arrow results need no pin.
Connection close also releases the parent and closes its readers. Release is
idempotent; ordinary reads after explicit release intentionally acquire fresh
statement snapshots. Do not release until all required tables are fully consumed
and validated.

Expiry/lease loss leaves the connection bound to a failed view. Metadata and
subsequent reads fail; a second acquire also fails until explicit release. A
retry may reuse the same view only while valid and authorized. Otherwise discard
the partial full refresh and start a new attempt with a new view. No durable
Airbyte checkpoint, process resume or exactly-once destination effect is implied.
After process death, heartbeat renewal stops and server ownership expires within
two minutes of its last acknowledged renewal; normal admission/collection prunes
expired pins and reclaims unreferenced manifests/segments. Cleanup failures leave
only bounded lease protection and existing reference-safe reclamation.

## Python ADBC example

```python
import adbc_driver_manager.dbapi

with adbc_driver_manager.dbapi.connect(
    driver="/path/to/libadbc_driver_redis.so",
    db_kwargs={"uri": "rediss://USER:PASSWORD@HOST:PORT/0"},
    autocommit=True,
) as conn:
    native = conn.adbc_connection
    native.set_options(**{"adbc.redis.read_snapshot_retention_ms": "3600000"})
    native.set_options(**{"adbc.redis.read_snapshot": "acquire"})
    try:
        # Discovery must happen after acquire, on this same connection.
        orders_schema = conn.adbc_get_table_schema("orders", db_schema_filter="public")
        items_schema = conn.adbc_get_table_schema("order_items", db_schema_filter="public")
        with conn.cursor() as cur:
            cur.execute("SELECT * FROM public.orders")
            orders = cur.fetch_arrow_table()  # finish all batches
            cur.execute("SELECT * FROM public.order_items")
            items = cur.fetch_arrow_table()
        # Validate schemas, exact values and counts before completing the sync.
        assert orders.schema == orders_schema
        assert items.schema == items_schema
    finally:
        native.set_options(**{"adbc.redis.read_snapshot": "release"})
```

Use a trusted TLS CA configured for the client; choose the platform's library
extension. Connector integration must feature-probe acquisition on its pinned
native connection, acquire before discovery, disable connection/partition
parallelism for the sync, fully consume/validate each result and release in
`finally`. Never degrade a requested shared snapshot to ordinary statements.
Pin the released driver version once its acceptance evidence exists.

## Flight SQL

Shared snapshots are **unsupported over Flight SQL**. Native ADBC option support
does not establish gateway support. A gateway would need an authenticated,
connection-affine metadata/query session, explicit acquire/release propagation,
fixed retention and cancellation/expiry ownership across RPCs, and rejection of
workers or requests that cannot preserve that session. No such transport
implementation or validation is claimed here. Airbyte using Flight SQL must
reject `read_consistency=shared_snapshot` until independently implemented/tested.

## Storage compatibility

This API uses existing protocol-17 reader pins and persists no new record format.
It requires an initialized protocol-17 managed catalog or fresh empty storage;
legacy catalogs have no in-place upgrade path. Existing compatible clients can
publish while this client retains its normal reader pin.
