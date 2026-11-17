# Arrow stream ownership gate (#446)

Inspection source: production commit `3671fca7`; Arrow Go v18.8.0 and driverbase
`4c82882827b5`, Go 1.27.3. This inventory bounds one ordinary host-memory
ExecuteQuery/Bind/BindStream bridge path. It does not cover device/GPU, every
extension layout or every connection/background-worker lifetime.

| Resource | Creation/transfer | Normal explicit cleanup | Remaining proof |
|---|---|---|---|
| `cStmt`, `cConn`, `cDatabase` | bridge stores `cgo.NewHandle` as `private_data` | release calls implementation Close and deletes handle | concurrent Close/execution and error paths across real clients |
| `cArrayStream` and exported reader | `exportRecordReader` retains reader and creates handle | `RedisArrayStreamRelease` clears callbacks/private_data, deletes handle, releases reader and owned error, then forces GC | repeated legal release and retained batches under sanitizers |
| Exported record buffers | stream get-next exports Arrow ArrayData; Arrow cdata retains data and allocates its own C handle holder | Arrow cdata `releaseExportedArray` releases ArrayData, deletes cgo handle and frees C holder/children/buffers | retained consumers after stream/statement/connection Close |
| Exported schema | cdata exports C strings, children and dictionary | Arrow schema release recursively frees C allocations | all nested/error layouts |
| Foreign Bind array | preflight then `ImportCRecordBatch`; statement Bind retains the record via reader | bridge defers record Release; rebind/Close releases statement reader | foreign producer callback balance on every refusal and last consumer |
| Foreign import allocator | tracks imported buffer references | last buffer Free invokes guarded `forceRelease` once, releasing foreign ArrowArray and C shell | unusual zero-buffer/error imports and consumer retention |
| Foreign BindStream reader | baseline `ImportCRecordReader` moves the stream into C shell; statement takes reader | reader refcount zero stops runtime cleanups, releases current batch, stream and pending array, frees shells | baseline donation violates Go ADBC retaining contract; paired correction below |
| Import fallback cleanups | `initReader` installs two `runtime.AddCleanup` callbacks | explicit reader Release stops both before freeing stream/array shells | cleanup/Release races and foreign callback thread requirements |
| Stream error | bridge owns allocated `AdbcError`; ErrorFromArrayStream lends it to caller | stream release releases/frees error | no consumer double-release of borrowed error |
| Managed stream/pin | production reader owns snapshot and Arrow owner | stream cleanup releases both through existing store lifecycle | real cancellation/Close, socket interruption and pool cleanup |

These implementation observations do not prove complete deterministic timing.
Arrow interface comments mention SetFinalizer, but v18.8.0's reader implementation
uses `runtime.AddCleanup`; audit the actual implementation, not those comments.
An empty result/schema is not an ownership exemption. The driver's opaque handle
encoding also differs from Arrow's C-allocated uintptr holder.

## Reproduced unchanged-policy gate

The new `build.py --base ... --baseline-only` mode archives an explicit production
commit and overlays only the existing nonshipping stub fixture. No omission patch
is applied. Normal and race builds succeeded on macOS arm64. The normal native
C/ctypes + PyArrow correctness probe passed all seven existing groups, retained
arrays/imported Bind/BindStream callbacks and error/SQLSTATE checks; checked Arrow
allocator bytes returned to zero. No release microbenchmark was run.

The race library aborts with exit 134 before those checks:
`checkptr: pointer arithmetic computed bad pointer value`, at `main.createHandle`
in `go/pkg/driver.go:262`, reached by `RedisFFIProbeNew`. This reproduces the
previously recorded blocker against the current source with the current policy.
Do not disable checkptr or infer that GC omission caused the failure.
[Sanitized build/check identities](ffi-safety.json) retain both outcomes. Full
logs and manifests are private because they include operational filesystem paths.

No ASAN/UBSAN, poison allocator, concurrent independent real-client lifecycle,
sustained retained Go/C memory accounting, cancellation/Close ownership gate or
production GC-policy comparison has passed here. The safety gate remains blocked;
all four production forced-GC calls remain. Upstream policy work remains
[driverbase-go #243](https://github.com/adbc-drivers/driverbase-go/issues/243).

## Paired retaining-contract correction

[Upstream review](https://github.com/adbc-drivers/driverbase-go/issues/243#issuecomment-6924390177)
identifies an explicit imported-reader release and retaining Go implementation as
prerequisites. The current Go ADBC v1.12.0 contract already requires the driver
to retain BindStream input and the caller to release its own reference. Baseline
statementImpl instead donated that reference and released it even on refusals;
the C bridge omitted Release to compensate. This also made self-rebinding and
ordinary Go callers that release after binding unsafe.

The paired correction retains before clearing an old binding (including the
same reader), preserves caller references on refusal, supports nil unbind, and
defers the imported reader's Release at the C boundary after a successful import.
Hash-copy and Go test callers keep/release their own references instead of
adding compensating retains. The historical stub fixture remains unchanged;
the builder adapts only the overlaid stub for an explicitly retaining bridge and
records its actual bytes/hash separately. Historical source defaults stay fixed.
This is an ownership correction while retaining every forced GC call; it does
not select a GC policy. Current validation is recorded in the change record.

The corrected source `6e17a91f` was also built/run in both modes: the normal
probe again passes seven groups with zero checked allocator bytes; the race
probe again aborts in unchanged createHandle before ownership scenarios. Its
adapted stub now retains BindStream input to match the corrected bridge. Exact
written-fixture and binary hashes are in ffi-safety.json. This closes the paired
reference correction's normal probe check, not the outstanding race/sanitizer gate.
