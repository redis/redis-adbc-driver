# Plan and validation

Date: 2026-11-17. Source: `292ee254`. Status: implemented; checks retained; review pending.
See [intent](intent.md) and [specification](spec.md).

1. Remove the stale toolchain pin, distinguish launcher/actual version and check
   archive member paths/types before Python 3.11 extraction.
2. Build the current merged bridge in new private Linux archives with strict
   checkptr=2/cgocheck2, and separately race. Keep production forced GC unchanged.
3. Run both original basic ownership groups and the complete expanded probe,
   retaining all attempts. Do not skip sliced STRUCT or relax callback assertions.
4. Publish sanitized evidence and explicit limits. Submit a separate reviewable
   PR for the builder fix, referencing #446. Do not close lifetime/performance work.

## Results and review

- The original forced-Go-1.27.3 selection reproduces the module minimum-version
  refusal with `go list -m`; no compilation can begin with that pin.
- Linux/aarch64 and macOS/arm64 archives build with actual Go 1.27.4 while
  recording the older launcher separately. Python 3.11 extraction succeeds.
- Linux checkptr=2/cgocheck2 basic probe passes all seven ownership groups with
  zero checked allocator bytes. macOS race/checkptr=2/cgocheck2 passes all seven.
- Expanded probes fail the same sliced STRUCT assertion under current pinned
  Arrow v18.9.0. An independent Go-only C Data reproduction also fails. The
  dependency blocker is retained; no assertion or required scenario was skipped.
- Both Linux race probes stop at ThreadSanitizer allocation before checks. The
  library builds, but no Linux race success is claimed. All attempts retained.
- Python compilation and diff whitespace checks pass. Author review inspected
  source-only archive selection, path/type rejection, actual toolchain identity,
  unchanged GC/default source and absence of private operational data. This is
  not maintainer approval.
- [Full sanitized results and limits](../../../../benchmarks/ffi-release-gc/2026-11-17-current/README.md).
  #446 remains open for the complete gates; no performance qualification.
