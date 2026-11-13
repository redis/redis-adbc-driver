# Forced-GC C-ABI attribution

The omission patch remains experimental. Both race libraries abort in the
unchanged `createHandle` integer-to-pointer conversion before ownership checks;
checkptr was not disabled. ASAN was not run. Normal probe checks and timing do
not establish complete lifetime or sanitizer safety, so the default driver keeps
all four forced collections. The native read control is pending.

The server-free C-ABI comparison passed seven scenario groups per variant:
explicit owner close once for each handle kind with invalid-state repeated calls,
retained exported arrays after stream/statement teardown, foreign Bind and
BindStream producer callbacks at last ownership release, and stream/close errors
with SQLSTATEs and cleanup. Checked fixture Arrow allocations returned to zero.
These stubs exercise the actual wrappers and Arrow C import/export, not Redis
pools, leases, background workers, cancellation or every Arrow/device type.

| Actual release entrypoint | Baseline median µs | Experimental median µs |
|---|---:|---:|
| Database | 328.281 | 0.712 |
| Connection | 296.031 | 0.758 |
| Statement | 296.817 | 0.755 |
| Stream | 513.896 | 0.877 |

Every timed sample released 128 prepared owners. All 28 baseline samples recorded
exactly 128 forced collections; all 28 experimental samples recorded zero. Each
of four cases includes one warmup and six measured observations per variant;
[all 56 observations](results.json) are retained. Setup and counter reads were
outside timing. The small live heap, Python/ctypes boundary and synchronous
cleanup are part of this attribution. It is not a pure GC microbenchmark or an
estimate of production query savings. A fresh process ran each variant, with the
baseline block first; order, host load and runtime heuristics limit causal scope.

Go was 1.27.3 on macOS arm64. The fixture libraries use physical archived Go
source corresponding to production base `24f654b7`, plus the separately gated
probe; the candidate additionally applies only the four-GC-call omission patch.
Archive builds have no embedded Git revision. The exact archive, fixture, module,
patch and binary hashes are in the results. Ordinary builds and dependencies
remain unchanged.

Retained negative attempts include a failed virtual-cgo overlay build (missing
generated C files), an initial candidate compile failure from removing `runtime`
while `runtime.Caller` still needed it, two controller name-resolution mistakes
before subprocess execution, and both race/checkptr SIGABRTs. The corrected patch
retains the runtime import and every non-GC runtime use. No failed timing was
retried or discarded, and no measurement existed before the compile correction.
