# Validation record

Date: 2026-11-13. Status: server-free attribution complete; native control pending.
Normal production Go files and module dependencies remain identical to base
`24f654b76b98f3d8960af94a3ca0230185d467a1`.

The [report](../../../../benchmarks/ffi-release-gc/2026-11-13/report.md) and
[all observations/provenance](../../../../benchmarks/ffi-release-gc/2026-11-13/results.json)
retain both variants' 56 actual C-ABI observations (four entrypoints, one warmup
and six measured samples each, 128 prepared owners/sample). All baseline forced-GC
deltas were 128; all omitted-GC deltas were zero. Both normal libraries passed
seven ownership/error scenario groups and returned fixture Arrow allocations to
zero. No failed timing was discarded or retried.

The race gate is **blocked**: both baseline and candidate race builds succeeded,
then both executions terminated with SIGABRT/exit -6 before ownership checks at
the existing `createHandle` integer-to-pointer conversion. Checkptr was not
disabled. ASAN was not run. Default forced-GC behavior remains unchanged; these
measurements are not sufficient grounds for production adoption.

The initial virtual-file cgo overlay build failed; archived physical Go source
builds succeeded. The initial candidate patch incorrectly removed the runtime
import while runtime.Caller still needed it; the corrected patch omits exactly
the four GC calls. Both compile failures and two controller name-resolution
mistakes before subprocess execution are retained alongside both race failures.

Source inspection verified explicit zero-reference Arrow imported array/stream
release, import-error cleanup, Go reader retention, statement parameter clearing
and export ownership in pinned Arrow v18.8.0 and this driver. The fixture directly
checks selected paths with real wrappers and foreign callbacks. It does not test
production Redis close/join, leases, retained snapshots, cancellation, all Arrow
types or GPU. No sanitizer or exhaustive lifetime proof is claimed.

Light evidence checks: `python3 benchmarks/ffi-release-gc/audit.py` passes all56
C-ABI sample matrices, medians, forced-GC counts, both blocked race outcomes and
source/module/fixture/runner/patch identities. Python source AST parsing passes.
The new public builder has not been executed; actual measurements used a private
controller and frozen archived trees. Further build/native execution requires the
parent's exclusive slot. Shared Redis was untouched by this slot.
