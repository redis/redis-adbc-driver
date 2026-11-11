# Native managed-table HASH client

Experimental source implementation for [#385](https://github.com/redis/redis-adbc-driver/issues/385).
Build the current shared library with `make -C go build`; this is not a claim
that existing released wheels contain the new native ABI.

The Python wrapper calls a shared Go snapshot reader directly. It does not
generate SQL, parse SQL, build a SQL plan, or execute SQL for reads. SQL and
supported Arrow ingestion remain the write interfaces.

```python
from adbc_driver_redis.native import NativeClient

with NativeClient(uri, decode_responses=True) as client:
    customers = client.table("customers", schema="public")
    print(customers.hgetall(1))
    print(customers.hmget(1, ["name", "country"]))
    print(customers.hget(1, "name"))
    print(customers.hexists(1, "name"))
    print(customers.hlen(1))
    print(customers.exists(1))
```

For a source checkout, put `python/` on `PYTHONPATH` and set
`REDIS_ADBC_LIBRARY` to the absolute built library path (`.dylib`, `.so`, or
`.dll` for the platform). The ordinary package dependencies apply. Native
HASH reads do not require PyArrow. Database options use string names/values
through `db_kwargs`, as with other package entry points.

## Identity and results

- Bind exact schema/table identifiers; no SQL quoting or key-string parsing is
  involved. Reads address positive int64 internal `__rowid` values. An
  application `customer_id=101` does not mean internal row ID 101. Obtain IDs
  through existing driver results, for example by including `__rowid` in a SQL
  query. Application-key mapping is not implemented.
- `hgetall` returns public column names and their stored Redis bytes. Renamed
  columns and metadata-backed added-column defaults follow the pinned schema.
  Internal row IDs, provenance, tombstones and NULL markers are hidden.
- SQL NULL and unknown fields are absent: `hget` returns `None`; `hmget` returns
  a list preserving field order and duplicates, with `None` for absent fields.
  Empty values are present. `hlen` counts non-NULL public fields. An all-NULL
  row can have `exists=True`, `hlen=0`, and `hgetall={}`.
- Missing rows produce empty HASH results, absent fields and `exists=False`.
  Missing tables, permission refusals and unavailable snapshot data raise errors.
- Default results use byte keys/values, with field-name arguments supplied as
  Python strings. `decode_responses=True` decodes values using `encoding`
  (default UTF-8) and returns string keys. Invalid text raises a decoding error.
  This is **not SQL type conversion**: booleans, dates, timestamps and other
  values use their driver storage encoding. Use ADBC/Arrow for typed values.

## Consistency and ownership

Each method call acquires and releases one committed snapshot. To reuse a
fixed catalog view across tables and calls:

```python
with NativeClient(uri) as client:
    with client.snapshot(retention_ms=900000) as view:
        customer = view.table("customers").hgetall(1)
        item = view.table("order_items").hgetall(1)
```

The context creates a dedicated native connection. Parent-client reads continue
using independent fresh snapshots. Exiting closes the view and releases its
pin even after a consumer exception. Table objects from a closed view fail on
reuse. Each client serializes calls; use independent clients for independent
concurrent views. Close clients explicitly or use their context managers.
There is no Python async/cancellation API in this initial binding. Close waits
for an active serialized Python call; backend timeouts and the native request
lifetime remain in force. The Go API accepts cancellation contexts and uses
the driver's execution transport for point-read interruption.

Snapshot lifetime is fixed at 120000..3600000 ms (default 900000). Existing
[shared snapshot authority and retention rules](shared-read-snapshots.md) apply:
lease renewal, terminal invalidation, live backend grants, catalog limits, and
cleanup/expiry on process death. Failures never switch to a newer view. The
boundary is the existing managed catalog in one Redis hash slot, not a
distributed snapshot or a multi-statement write transaction.

Native reads share connection initialization, descriptor/index readiness
proofs, strict directory membership, row-version/tombstone resolution, server
fences, and final pin validation with existing managed read paths. Backend
authorization is enforced using the caller's authenticated connection; the
wrapper does not introduce a separate authorization system. Backend
reader-control writes are required even though the public API is read-only.
RESP3, TLS, ownership/noeviction requirements and storage protocol 17 remain
unchanged. Do not mutate managed physical HASHes or indexes directly.

Each call reads one row across the bounded segment manifest. At most 4096
requested/public projected fields and 64 MiB of returned field-name/value bytes
are accepted. Errors return no partial result. These are logical result bounds,
not an RSS ceiling: Redis replies, Go/Python copies, metadata and the binding's
base64 JSON envelope add memory overhead. Per-call native lifetime is at most
the existing 15-minute snapshot timeout, further bounded by backend I/O timeouts
and any supplied Go context.

## Native Go API and binding

`OpenNativeReader(ctx, options)` returns an owned `*NativeReader` with `ReadHash`,
`HGetAll`, `HMGet`, `HGet`, `HExists`, `HLen`, `Exists`, `AcquireSnapshot`,
`ReleaseSnapshot`, and `Close`. `ReadHash` distinguishes missing rows from
all-NULL rows using its separate existence result.

Python uses the experimental `RedisNativeReadV1` / `RedisNativeReadFreeV1`
shared-library symbols. Requests use registered opaque integer reader IDs;
results use base64 for binary values. Go pointers are never exported. This
private binding ABI is not a Redis protocol endpoint or an ADBC option.

Unsupported initially: application-primary-key mappings, Redis command
passthrough, writes, SQL views/temporary tables, foreign-index discovery mode,
retained-partition reader mode, Search, scanning, pipelines, and drop-in redis-py
compatibility. Imported managed copies are readable as managed rows; their
external source HASHes are separate. Search is a follow-up requiring its own
visibility/count/order/pagination contract.
