# C-ABI release GC attribution

This is an experimental fixture, not a production GC policy change. Normal driver
builds are unchanged. The [change record](../../docs/sdlc/changes/ffi-release-gc-attribution/intent.md)
defines the ownership gates and evidence limits.

`probe.go.txt` is overlaid as `go/pkg/ffi_release_probe.go` and compiled only with
`-tags driverlib,ffireleaseprobe`. It constructs interface stubs inside the actual
C wrapper handle types, never a Redis client. Unimplemented operations fail rather
than contact a server. Its streams use the real Arrow importer/exporter and the
normal `RedisStatementBind`, `RedisStatementBindStream`, execute and release ABI.
It is deliberately absent from shipping libraries.

The comparison overlay applies [omit-forced-gc.patch](patches/omit-forced-gc.patch)
to a private copy of `go/pkg/driver.go`. Exactly the four forced collections are
omitted; the comments explaining the existing ASAN rationale are retained as
historical context. Do not apply this patch to a release without additional
sanitizer and production ownership review.

Build **both** libraries before either measured block, preserving the Makefile's
version linker flag. Then run each in a fresh process with the same Python,
PyArrow, environment, iteration and repeat settings:

```sh
python benchmarks/ffi-release-gc/run.py --library /private/baseline.dylib --output /private/baseline.json
python benchmarks/ffi-release-gc/run.py --library /private/candidate.dylib --output /private/candidate.json
```

The default four cases have one warmup and six measured samples, 128 releases per
sample. Setup and GC counter reads are outside timing; ABI transitions, explicit
cleanup and baseline whole-runtime GC are inside. Forced GC deltas distinguish
synchronous wrapper collections from ordinary runtime heuristics. The small
server-free fixture measures wrapper attribution, not query latency or realistic
heap-dependent GC cost. A race build is a correctness check, never a timing
variant. Python producer callback counts and retained arrays check selected
ownership paths; they do not establish ASAN success, all data types, device/GPU
ownership, connection pool or background worker safety.
