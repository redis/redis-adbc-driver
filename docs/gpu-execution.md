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

Supported physical layouts are nullable primitive boolean, signed/unsigned
8/16/32/64-bit integers, float32/float64, or a flat record batch containing those
types. Root struct offsets must be zero; sliced child arrays are supported.
Strings, decimals, timestamps, nested structures and dictionaries return clear
unsupported errors. Each staging batch is bounded to 512 MiB including validity
buffers; invalid lengths and size overflow are rejected. Metadata lives on CPU;
callers must follow the normal C interface validity/lifetime contract.

Never pass device pointers to standard ADBC Bind or BindStream as if they were
CPU pointers. Those interfaces have no device discriminator. Use the explicit
C Device entry point or stage to host first.

## Validation and performance scope

Native tests are in [test_native_device.py](../benchmarks/gpu-query/test_native_device.py).
CPU ownership/rejection checks need `REDIS_ADBC_LIBRARY`. CUDA/Redis tests also
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
