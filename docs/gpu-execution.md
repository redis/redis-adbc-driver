# Experimental CUDA predicates and Arrow C Device interchange

Issue [#215](https://github.com/redis/redis-adbc-driver/issues/215).
Status: experimental; validated on NVIDIA A10G. CPU execution remains the default.
See [validation](sdlc/changes/215-gpu/validation.md) for tested cases and limits.

## CUDA execution

The native driver can evaluate an integer WHERE comparison on CUDA. Row fetching,
decoding, projection, grouping and exact aggregate accumulation remain on CPU.
This is a GPU predicate backend, not a general GPU SQL engine. For example:

```python
cur.adbc_statement.set_options(**{
    "adbc.redis.execution_backend": "cuda",
    "adbc.redis.cuda_device": 0,
})
cur.execute("SELECT bucket, COUNT(value), MIN(value), MAX(value) "
            "FROM gpu_fixture WHERE value >= 0 GROUP BY bucket")
table = cur.fetch_arrow_table()
```

Supported queries read one physical table, require a predicate of the form
`integer_column operator integer_literal`, and project integer columns or
COUNT/SUM/AVG/MIN/MAX of integer columns. Operators are `=`, `!=`, `<>`, `<`,
`<=`, `>` and `>=`. Integer GROUP BY columns are supported. Other query shapes,
including joins, CTEs, derived tables, windows, DISTINCT, ORDER BY/LIMIT, complex
predicates and missing WHERE clauses, return NOT_IMPLEMENTED when CUDA is
requested. The current predicate compiler requires a non-NULL integer literal
on the right; it does not coerce strings or accept bound parameters there.

CUDA evaluates at most 65,536 decoded values per launch, preserves input order
and treats NULL comparisons as non-matching. Integer SUM overflow and exact
floating AVG behavior remain those of the existing CPU accumulators. No SQL
arithmetic is replaced with wrapping GPU arithmetic.

Statement options:

| Option | Values | Default |
|---|---|---|
| `adbc.redis.execution_backend` | `cpu`, `cuda` | `cpu` |
| `adbc.redis.cuda_device` | Nonnegative CUDA device ordinal | `0` |
| `adbc.redis.gpu_metrics` | Read-only JSON: kernel launches and input rows | `{}` |

The forced CUDA path scans candidates and performs the supported predicate on
the GPU rather than hiding it behind server aggregate/filter pushdown. It
materializes the SQL result and does not support `stream_required` or partition
execution. CUDA initialization, launch or copy failures are errors, with no
silent CPU fallback. Select `execution_backend=cpu` explicitly to use the
portable path and its existing pushdown/worker choices. Cancellation is checked
before and after each bounded CUDA batch; a running kernel is not preempted.

The runtime requires a Linux cgo build, an NVIDIA GPU/driver, CUDA runtime
(`libcudart.so.12` or `.13`), NVRTC (`libnvrtc.so.12` or `.13`) and `libcuda.so.1`
on the dynamic library search path. Libraries are loaded only when requested;
CPU execution does not require NVIDIA libraries. The non-cgo CLI still builds.
macOS supports CPU execution and CPU-device staging; CUDA requests report an
explicit unsupported-platform error. This implementation does not use Metal.

## C Device API

The standard ADBC v1.1 function table still uses CPU ArrowArray/ArrowArrayStream.
The driver additionally exposes the following symbols, declared in
[`go/pkg/device.h`](../go/pkg/device.h):

```c
int RedisDeviceArrayFromHost(const struct ArrowArray* input,
    const struct ArrowSchema* schema, int64_t device,
    struct ArrowDeviceArray* out, char* error, size_t capacity);
int RedisDeviceArrayToHost(const struct ArrowDeviceArray* input,
    const struct ArrowSchema* schema, struct ArrowArray* out,
    char* error, size_t capacity);
AdbcStatusCode RedisStatementExecuteDevice(struct AdbcStatement* stmt,
    int64_t device, struct ArrowDeviceArrayStream* out,
    int64_t* affected, struct AdbcError* error);
AdbcStatusCode RedisStatementBindDevice(struct AdbcStatement* stmt,
    struct ArrowDeviceArray* input, struct ArrowSchema* schema,
    struct AdbcError* error);
```

The standalone copy helpers accept borrowed descriptors and return errno-style
codes (zero on success). Statement extensions return ADBC status/error objects.
All output structs must be zero initialized and released before reuse.

Statement extensions require **driver-owned native handles** created through
the driver's RedisDatabase/Connection/Statement functions. Do not pass a driver
manager's wrapper handles directly to these extension symbols. Managers without
an extension dispatch mechanism can use the standalone array copy helpers and
their ordinary CPU Bind/ExecuteQuery APIs. The test harness demonstrates native
handle creation; no standard ADBC C Device function-table slots are invented.
ExecuteDevice accepts one SELECT without WITH; updates and ingestion use the
ordinary CPU execution entry point after optional BindDevice. DML is rejected
before execution so an unsupported device-output request cannot write rows.

Import accepts CPU or CUDA descriptors. CUDA imports wait on a supplied
`sync_event` (a CPU pointer to a `cudaEvent_t` handle) before copying device
buffers into owned host buffers. Inputs and schema are borrowed, never consumed;
the producer must retain descriptors, event and buffers until the call returns.
CPU descriptors must not supply CUDA events. Device IDs are validated before
CUDA access. Metal and other device types return unsupported errors before any
attempt to read their data buffers.

BindDevice polls producer readiness with its statement cancellation context;
cancellation interrupts that wait without consuming the producer's descriptors.
It does not cancel producer-owned GPU work. The producer remains responsible
for completing that work before freeing its own buffers/event. Standalone copy
helpers have no statement context and synchronously wait for producer readiness.

Export copies host buffers to owned CUDA allocations on the requested device.
Copies complete synchronously, so output `sync_event` is NULL. A returned batch
owns its buffers independently of the source CPU batch and the device stream;
it remains valid after either is released. Call `array.release` on every device
batch, `schema.release` on every exported schema and `stream.release` on the
stream, including early-close/error paths. BindDevice stages to host before
passing data to ordinary binding, so the producer can release its buffers after
BindDevice returns. Normal statement cancellation propagates to the underlying
CPU result stream.

Physical interchange support is separate from Redis SQL storage and CUDA
predicate support. A staging-supported physical type does not enable GPU SQL
operations on that type.

| Arrow physical layout / format | V1 staging | Slice and metadata policy |
|---|---|---|
| Boolean `b`, integers `cCsSiIlL`, float32/64 `fg` | CPU/CUDA | Child slices retained; nullable |
| UTF-8 `u`, binary `z` | CPU/CUDA | Signed 32-bit offsets; slices normalized to zero |
| Large UTF-8 `U`, large binary `Z` | CPU/CUDA | Signed 64-bit offsets; slices normalized to zero |
| Flat record batch `+s` | CPU/CUDA | At most 1,024 primitive/variable children; root offset zero; children at least root length |
| Decimal, timestamps/time/duration, fixed binary, float16 | Unsupported | Metadata/precision/time-zone semantics deferred |
| Dictionary and extension storage requiring dictionaries | Unsupported | Dictionary ownership and index validation deferred |
| List, map, union, nested struct, run-end and view layouts | Unsupported | Recursive layout semantics deferred |

Schema names, flags and metadata remain CPU-resident. The Python adapter copies
schema metadata into its returned Arrow object. Variable-width staging reads
only the selected offsets and payload, validates nonnegative monotonic offsets
before accessing payload, rebases offsets and shifts the validity bitmap. Empty
values may have a NULL data buffer when the selected payload size is zero.
Offsets are required even for an empty variable array. NULL validity buffers
mean all-valid when the descriptor does not report positive NULL count.

Each batch has a 512 MiB staging budget, including buffers and temporary host
offset/validity storage. Length/offset arithmetic is checked before allocation;
selected data size is checked before data access. CPU/CUDA buffers are copied
with the appropriate device operation. Traversal is bounded to a root and one
child level; unsupported schemas are rejected before buffer access. Like the C
Data Interface itself, this API has no buffer capacity fields: producers must
supply valid accessible buffers for their advertised descriptor ranges.

The staged plan is (1) numeric/boolean (existing), (2) UTF-8/binary and large
offsets (this extension), (3) separately specify decimal precision/scale and
temporal unit/time-zone preservation, (4) separately specify dictionary index,
dictionary lifetime and nested child-offset semantics. Stages 3/4 remain
unsupported until implemented and validated. No recursive fallback is enabled.

Never pass device pointers to standard ADBC Bind or BindStream as if they were
CPU pointers. Those interfaces have no device discriminator. Use the explicit
C Device entry point or stage to host first.

## Python and driver-manager adapter V1

`adbc_driver_redis.device.DeviceAdapterV1` uses standalone staging helpers and
ordinary public manager capsule binding. It never passes manager handles to
native extensions and adds no ADBC function-table slots. Discovery calls
`RedisDeviceInterfaceVersion()` and requires version 1 and its staging/event
symbols. Older libraries produce an upgrade/CPU-path error. The adapter accepts
manager 1.8..1.x and PyArrow 14..26; the recorded local test combination is
CPython 3.15, manager 1.13.0, PyArrow 26.0.1 on macOS ARM64. A real Linux
amd64/A10G run also passed on CPython 3.12.3, manager 1.13.0,
PyArrow 26.0.1, CuPy 14.2.0, pylibcudf 26.10.2 and CUDA 12.9. Other combinations
in those ranges need deployment validation. Missing/out-of-range dependencies
must be resolved before using this optional adapter. Importing the normal CPU
client or device module loads no CUDA Python package; PyArrow is loaded only
when constructing an adapter.

```python
from adbc_driver_redis.device import DeviceAdapterV1

adapter = DeviceAdapterV1()
# producer implements __arrow_c_device_array__ and returns fresh named capsules.
# CUDA ordinals are explicit and must match the producer descriptor.
cpu_batch = adapter.to_host(producer, device_type=2, device_id=0)
# A manager AdbcStatement uses the ordinary CPU capsule Bind API:
adapter.bind(statement, producer, device_type=2, device_id=0)
# For an unnamed raw pylibcudf.Table, also pass column_names=["text", "x"].
# Producer may be released here, before statement.execute_update/query().

# For CPU results: fully fetch through ordinary ADBC, then export each batch.
device_producer = adapter.to_device(cpu_batch, device_id=0)
# Optional real consumer; imported only by the application:
# gpu_table = pylibcudf.Table.from_arrow(device_producer)
```

Raw `pylibcudf.Table` device capsules export unnamed fields in the tested
26.10.2 version. The adapter preserves the producer schema; it does not infer
application names. For manager ingestion, pass `column_names=[...]` to `bind`
with one distinct nonempty name per column. For `to_host` results, compare by
column position or explicitly rename columns before making row dictionaries;
duplicate field names can otherwise hide columns in those dictionaries.

`to_host` returns an owned PyArrow Array or RecordBatch. Inputs, schema and events
are borrowed for the call; capsules and producer stay alive until it returns.
No borrowed release callback is consumed. Each `to_device` capsule export copies
an Array/RecordBatch to independent owned CUDA allocations, with a CPU schema.
Capsule destructors release unused exports; consumers move release callbacks
according to the Arrow capsule protocol. Exported capsules survive deletion of
the producer, and each export completes synchronously with a NULL sync event.
Requested-schema conversion is explicitly unsupported.

`to_host` and `bind` accept `cancel=threading.Event()`. Readiness polling uses
`RedisDeviceArrayEventReady` (zero when ready, `EAGAIN` while pending), checking
cancellation between polls and around bounded synchronous copies. Cancellation
raises `concurrent.futures.CancelledError`, releases temporary outputs and leaves
producer ownership intact. It cannot preempt a copy or terminate producer work;
the producer must finish that work before freeing its buffers/event. Cancel
subsequent SQL execution through the public manager statement `cancel()` API.

Errors include unsupported device/layout, mismatched device ID, malformed
offsets, batch budget, unavailable CUDA or missing extensions. There is no
implicit fallback on device failure. The explicit CPU path is ordinary PyArrow
CPU arrays/batches with standard ADBC Bind/BindStream/ExecuteQuery, or
`to_host(cpu_producer, device_type=1, device_id=-1)` for CPU device capsules.
These adapters copy/stage through host memory; they do not provide zero-copy
GPU execution. Contracts follow the [Arrow C Device interface](https://arrow.apache.org/docs/format/CDeviceDataInterface.html)
and [public manager capsule Bind API](https://arrow.apache.org/adbc/main/python/api/adbc_driver_manager.html#adbc_driver_manager.AdbcStatement.bind).

## Validation and performance scope

Native tests are in [test_native_device.py](../benchmarks/gpu-query/test_native_device.py).
CPU ownership/rejection checks need `REDIS_ADBC_LIBRARY`. Public adapter tests
are in [test_device_adapter.py](../benchmarks/gpu-query/test_device_adapter.py).
See [extended validation evidence](sdlc/changes/293-294-device/validation.md) for
the actual A10G run, versions, initial failure/correction and explicit skips.
CUDA/Redis tests also
need `GPU_TEST=1` and a verified Redis Software/4x-QPF `REDIS_URI`. Tests cover
native SQL against CPU results, exact large integers, overflow, real CuPy device
buffers/events, a real Arrow C Device consumer, and arrays retained past stream
release. Operational endpoints and credentials must stay outside committed logs.

The [earlier benchmark](../benchmarks/gpu-query/README.md) measured a different
consumer-side cuDF prototype at its recorded source revision. Its results do not
validate this native implementation or establish a native speedup. No automatic
GPU cost gate or universal query performance improvement is claimed.

The separate [CPU integer SIMD experiment](../benchmarks/integer-simd/README.md)
measures exact comparison masks and decoded-map conversion without changing SQL
execution or the CUDA ABI. Its recorded outcome is no-go for automatic batching:
conversion costs outweigh the resident-kernel gains on the tested workloads.
