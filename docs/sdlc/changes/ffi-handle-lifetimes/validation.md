# Validation

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Baseline: the previously retained C-ABI race probe aborts in createHandle before
any ownership scenarios. The unchanged Go 1.27.3 runtime/cgo source explicitly
states that coercing an integer Handle to unsafe.Pointer is unsafe. Arrow Go
v18.8.0 cdata uses a C uintptr holder with explicit free on release.

Current implementation preserves all forced GC calls. Results are below;
passing a focused holder test is not full Arrow lifetime or sanitizer acceptance.

The focused driverlib test initially exposed three pre-existing vet failures:
nonconstant text passed as setErr's format argument. Those calls now pass `%s`
with the message as data, preserving literal percent markers. Vet stays enabled.
`go test -count=1 -race -gcflags=all=-d=checkptr=2 -tags driverlib ./pkg`
passes, including GC survival, registry deletion and 2,048 independent concurrent
holder lifetimes. This focused result does not establish sanitizer acceptance.

## Follow-up results (2026-11-17)

Source for the native fixture: `5072b47e`, unchanged forced-GC policy. Host:
macOS, native local C/ctypes/PyArrow, server-free stubs; no Redis/network timing.
Private source/binary/harness hashes are retained in build manifests. These are
correctness checks, not production query or performance qualification.

- The focused holder command also passes with `GOEXPERIMENT=cgocheck2`.
  CI now runs that driverlib race/checkptr/cgocheck2 gate explicitly; ordinary
  untagged `go test ./...` does not select these bridge tests.
- The original native seven ownership groups pass with `--race --checkptr
  --cgo-check`, with no disabled pointer checks.
- The expanded native lifetime probe **fails with pinned Arrow Go v18.8.0**:
  sliced STRUCT values are shifted/truncated. The independent pure Go C Data
  reproduction expects `{[202 303]}` but receives `{[303]}`. Keep this assertion.
- A private Arrow STRUCT exporter patch using unsliced child data passes that
  reproduction and the expanded strict native probe: 12 groups, 256 rebind cycles
  across four independent clients, exact callback balance, zero checked Arrow
  allocator bytes. The pinned project dependency remains unchanged.
- The private patched Arrow C Data suite passes with
  `go test -count=1 -p 1 -race -gcflags=all=-d=checkptr=2 -tags test ./arrow/cdata`.
  An earlier command without `-tags test` selected no tests; it is not evidence.
- The original seven native groups also pass in separate Linux/arm64 container
  ASAN and UBSAN builds, using pinned Arrow, Go 1.27.3, Python 3.11 and PyArrow
  26.0.1. Both preserve the driverbase version linker flag. ASAN uses Go `-asan`
  and preloaded libasan with `detect_leaks=0`; this is not a leak gate. UBSAN uses
  `-fsanitize=undefined -fno-sanitize-recover=all -fno-omit-frame-pointer` native
  compile flags, `-fsanitize=undefined` link flags and `halt_on_error=1`.
  These smoke checks do not cover the extended nested/poison/concurrency suite
  under sanitizers or establish full lifetime acceptance. Source/binary/command
  hashes are retained privately; the task-owned container was removed.
- Local `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`
  and `make -C go build` pass. No live endpoint was supplied; integration skips
  do not establish Software compatibility. PR CI owns the live fixtures.

Upstream reports were submitted and linked from labeled project trackers:
[driverbase #245](https://github.com/adbc-drivers/driverbase-go/issues/245) /
[local #458](https://github.com/redis/redis-adbc-driver/issues/458),
[driverbase #246](https://github.com/adbc-drivers/driverbase-go/issues/246) /
[local #459](https://github.com/redis/redis-adbc-driver/issues/459), and
[Arrow #1598](https://github.com/apache/arrow-go/issues/1598) /
[local #460](https://github.com/redis/redis-adbc-driver/issues/460).

The experimental override is not shipping-driver acceptance. Full ASAN/UBSAN, real
C-driver-manager/Redis round trips, bounded sustained retained Go/C memory
accounting and a qualifying full-path GC-policy comparison remain open. Maximum
RSS and a checked Go Arrow allocator alone do not prove absence of native leaks.
All four `runtime.GC()` calls remain; #446 stays open.
