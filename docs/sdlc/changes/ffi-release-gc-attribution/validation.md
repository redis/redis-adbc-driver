# Validation record

Date: 2026-11-13. Status: evidence complete; final shared fixture teardown pending.
Default production Go files and module dependencies remain identical to base
`24f654b76b98f3d8960af94a3ca0230185d467a1`.

The [report](../../../../benchmarks/ffi-release-gc/2026-11-13/report.md) leads with
**no established native gain**: all four GC-omission medians were slower (+0.1%
to +4.1%), with overlapping elapsed ranges and small RSS changes. All 56 complete
native outputs validated against the independent 100K×105 typed source. Every
case retained a warmup plus six measured trials per variant. No failed, retried,
restarted or discarded native/C-ABI timing exists.

The separate server-free comparison retained 56 actual C-ABI observations: four
entrypoints, one warmup and six measured samples each, 128 prepared owners/sample.
All baseline forced-GC deltas were 128; all omitted-GC deltas were zero. Both
normal libraries passed seven ownership/error scenario groups and returned
fixture Arrow allocations to zero. This demonstrates selected explicit callback
ownership and per-release attribution, not a whole-query improvement.

The race gate is **blocked**: both race builds succeeded, then both executions
terminated with SIGABRT/exit -6 before ownership checks at the existing
`createHandle` integer-to-pointer conversion. Checkptr was not disabled. ASAN
was not run. Default removal is rejected on performance evidence and unresolved
safety gates; no production ownership-suite result is claimed.

The initial virtual-file cgo overlay build failed; archived physical Go source
builds succeeded. The initial candidate patch incorrectly removed the runtime
import while runtime.Caller still needed it; the corrected patch omits exactly
the four GC calls. Both compile failures and two controller name-resolution
mistakes before subprocess execution remain retained alongside both race failures.
No measurement existed before the compile correction.

[All observations/provenance](../../../../benchmarks/ffi-release-gc/2026-11-13/results.json)
distinguish probe-added physical archive builds (no embedded Git identity) from
the native GC-only clean private commit `b519284bcedd9ad24c48fbc53219b818dde3f68e`.
Native candidate contains no probe source/API and changes only four calls in
`go/pkg/driver.go`; modules/harness stay unchanged. Actual source, module,
fixture, runner, patch, archive, library/CLI hashes and build settings are recorded.

Source inspection verified explicit zero-reference Arrow imported array/stream
release, import-error cleanup, Go reader retention, statement parameter clearing
and export ownership in pinned Arrow v18.8.0 and this driver. The fixture checks
selected paths with real wrappers and foreign callbacks. It does not prove
production Redis close/join, lease/snapshot/cancellation safety, all Arrow types
or GPU ownership. No sanitizer or exhaustive lifetime proof is claimed.

Light evidence checks: `python3 benchmarks/ffi-release-gc/audit.py` recomputes
all 112 C-ABI/native observations, complete matrices, elapsed/RSS/first-batch
summaries, forced-GC counts, both blocked race outcomes and exact source/module/
fixture/runner/patch/build identities. Python source AST parsing, all relative
Markdown links, scoped privacy scan, patch apply with whitespace checking, and
scoped `git diff --check` passed (nested unified-diff patch excluded from outer
whitespace check). The public builder has syntax/source review only; actual
measurements used retained private controllers. No additional Go tests/builds
were run after native END.

The parent-owned Software fixture was independently reverified for active/effective
4x QPF, six workers/mnp32, one 4-GiB noeviction shard, strict data TLS/RESP3 and
actual four-CPU/eight-GiB limits. Both native blocks returned DBSIZE0/FT._LIST[]
with clients/processes closed. Parent owns final shared infrastructure/TLS/
credential teardown after sibling tracks finish; cleanup remains pending.
No ClickBench or alternate provider was used.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
