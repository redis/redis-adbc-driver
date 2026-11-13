# C-ABI release GC attribution

This is an experimental fixture, not a production GC policy change. Normal driver
builds are unchanged. The [change record](../../docs/sdlc/changes/ffi-release-gc-attribution/intent.md)
defines the ownership gates and evidence limits.

`probe.go.txt` is copied into a private archived Go tree as
`go/pkg/ffi_release_probe.go` and compiled only with
`-tags driverlib,ffireleaseprobe`. It constructs interface stubs inside the actual
C wrapper handle types, never a Redis client. Unimplemented operations fail rather
than contact a server. Its streams use the real Arrow importer/exporter and the
normal `RedisStatementBind`, `RedisStatementBindStream`, execute and release ABI.
It is deliberately absent from shipping libraries.

The comparison private tree applies [omit-forced-gc.patch](patches/omit-forced-gc.patch)
to a private copy of `go/pkg/driver.go`. Exactly the four forced collections are
omitted; the comments explaining the existing ASAN rationale are retained as
historical context. Do not apply this patch to a release without additional
sanitizer and production ownership review.

Build **both** libraries before either measured block, preserving the Makefile's
version linker flag. The builder requires a new private directory outside Git:

```sh
python benchmarks/ffi-release-gc/build.py --work /private/new-ffi-probe
```

It writes actual binary, source, module, fixture and patch hashes plus Go build
metadata to a private manifest. Archive builds have no Git revision embedded.
Then run each in a fresh process with the same Python,
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


Go/cgo rejected a virtual-file overlay in the first build attempt because generated
C files were missing. The successful fixture libraries use physical archived Go
trees; their build metadata has no Git revision. Their exact source base, fixture,
module and patch hashes identify them. The separate native candidate is a clean
private Git worktree containing only the four-call patch, without probe APIs.

The first candidate experiment patch also removed `runtime`, which is still used
by `runtime.Caller`; that compile failure is retained. The corrected patch leaves
the import and every other runtime use intact. Both normal fixture variants passed
seven ownership scenario groups. Both race variants abort before those scenarios
because checkptr rejects the existing integer-handle-to-pointer conversion in
`createHandle`. This blocked gate is preserved; checkptr was not disabled, and no
ASAN or complete lifetime safety is claimed.
