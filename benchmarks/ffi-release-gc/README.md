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

The [complete results](2026-11-13/report.md) include a separate native read
control: all four experimental medians were slower, despite the isolated release
cost reduction. Both negative native results and blocked safety gates keep the
default unchanged.

To reproduce the native control, create two private Git worktrees at
`24f654b76b98f3d8960af94a3ca0230185d467a1`. Apply the supplied omission patch only
to the candidate and commit that experimental change locally so its real clean
source identity appears in Go build metadata. The original measured native
candidate was private commit `b519284b`; a new local commit need not have the
same hash. Do not copy the probe into either native tree. Build both before any
timing, with the version flags preserved:

```sh
make -C /private/ffi-baseline/go build adbc-cli VERSION=v0.1.0-dev.next-four-base
make -C /private/ffi-candidate/go build adbc-cli VERSION=v0.1.0-dev.ffi-gc-none
```

Record actual library/CLI SHA256 and `go version -m`, source/module/patch/harness
hashes, host and environment settings in a private manifest. Use the independently
typed IPC source matching `source_sha256` in results. Provision and verify an
owned Redis Software fixture through `make -C go redis-up`; independently verify
effective 4x QPF/six workers/mnp32, noeviction and container resources. Configure
private `REDIS_BENCH_*`, `SSL_CERT_FILE` and retained-control policy only after
verification. The data URI must use TLS with certificate/hostname verification
and explicit `protocol=3`. Never use a shared application database.

Run the unchanged existing helper once for each variant, baseline first, with
fresh private output directories and matching explicit versions:

```sh
python benchmarks/table-modes/managed_read_memory.py --source /private/source.arrow --driver /private/ffi-baseline/go/build/libadbc_driver_redis.dylib --cli /private/ffi-baseline/go/build/redis-adbc --version v0.1.0-dev.next-four-base --work /private/baseline-results --dedicated-empty-server --read-repeats 6
python benchmarks/table-modes/managed_read_memory.py --source /private/source.arrow --driver /private/ffi-candidate/go/build/libadbc_driver_redis.dylib --cli /private/ffi-candidate/go/build/redis-adbc --version v0.1.0-dev.ffi-gc-none --work /private/candidate-results --dedicated-empty-server --read-repeats 6
```

The helper completely validates every output and returns its owned database
empty. Preserve every attempt and the declared order; do not repeat until a
preferred outcome appears. Independently verify cleanup and remove only the
fixture resources that the run owns. Operational manifests, endpoints, TLS keys,
credentials and raw management output stay private. This reproduction is not an
ASAN or passing race-gate procedure.
