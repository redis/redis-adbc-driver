# Forced-GC C-ABI attribution

Omitting the four forced collections **did not improve complete native reads**:
all four candidate medians were slower (+0.1% to +4.1%), with overlapping elapsed
ranges and small RSS differences. The default driver keeps all four calls.
Both race variants also abort at the unchanged `createHandle` conversion before
ownership checks; checkptr was not disabled, and ASAN was not run. The patch
remains an attribution experiment with unresolved safety gates.

## Complete native control

| Readers | Batch rows | Baseline median s (range) | Omit-GC median s (range) | Change | Baseline → omit-GC median maximum-worker RSS MiB |
|---:|---:|---|---|---:|---:|
| 1 | 512 | 2.392 (2.245–2.489) | 2.415 (2.249–2.540) | +1.0% | 252.15 → 252.09 |
| 1 | 4096 | 2.312 (2.226–2.471) | 2.406 (2.281–2.504) | +4.1% | 254.55 → 255.45 |
| 4 | 512 | 1.540 (1.471–1.635) | 1.541 (1.529–1.651) | +0.1% | 253.98 → 254.11 |
| 4 | 4096 | 1.503 (1.371–1.549) | 1.519 (1.421–1.595) | +1.1% | 256.11 → 256.52 |

All 56 complete outputs passed independent physical-type/value/count/multiplicity
validation against the same 100K-row ×105-column NULL-free Arrow source. Each of
four cases retained one warmup plus six measured trials per variant, with no
failed, retried, restarted or discarded native trials. The four-reader case has
eight disjoint tasks; the one-reader control has one task. Every sample, first
batch latency, elapsed/RSS distribution and exact provenance is in
[results.json](results.json).

The unchanged `managed_read_memory.py` and shared validators include spawned
worker startup, imports, source loading, connection/query/Arrow IPC work and
teardown in elapsed time; independent full-output comparison and setup are
outside timing. Maximum worker lifetime RSS includes Python/native/Go memory;
it is neither isolated Go heap nor the sum of concurrent worker memory.

Both libraries/CLIs were built before the baseline28→candidate28 timing blocks.
Go native GOMAXPROCS was explicitly unset. Block order was not randomized or
interleaved; shared-host/time/order and short-run variability limit interpretation.
No causal explanation for the small slower medians is established. An isolated
release saving must not be presented as a query speedup.

## Server-free C-ABI attribution

| Actual release entrypoint | Baseline median µs | Experimental median µs |
|---|---:|---:|
| Database | 328.281 | 0.712 |
| Connection | 296.031 | 0.758 |
| Statement | 296.817 | 0.755 |
| Stream | 513.896 | 0.877 |

Every sequential timed sample released 128 prepared owners. All 28 baseline
samples recorded exactly 128 forced collections; all 28 experimental samples
recorded zero. Four cases each retained one warmup plus six measured samples per
variant. All 56 C-ABI observations are preserved, separately from the 56 native
read trials. Setup and runtime counter reads were outside timing. The small live
heap, Python/ctypes boundary and synchronous cleanup are part of this attribution;
it does not measure concurrent callers or realistic heap-dependent GC costs.
Fresh processes ran baseline first, then candidate.

Both normal libraries passed seven scenario groups: explicit owner close once
for each handle kind with invalid-state repeated calls, retained exported arrays
after stream/statement teardown, foreign Bind and BindStream producer callbacks
at last ownership release, and stream/close errors with SQLSTATEs and cleanup.
Checked fixture Arrow allocations returned to zero. The Arrow consumer helper
skips cleared release pointers rather than calling a saved callback twice.

These interface stubs exercise actual wrappers and Arrow C import/export, not
Redis pools, leases, background workers, production statement cleanup,
cancellation or every Arrow/device type. Passing those selected scenarios is
insufficient to prove complete lifetime or sanitizer safety.

## Exact source and environment

Default production code/modules remain identical to
`24f654b76b98f3d8960af94a3ca0230185d467a1`.

* The C-ABI fixture libraries use physical archived Go source from experiment
  commit `9d6fb1af` (production Go identical to 24f), plus the gated fixture. The
  candidate additionally applies the corrected four-GC-call omission patch from
  `c84462f7`. Archive builds have no embedded Git revision. Exact archive,
  fixture, source, module, patch, binary and build-setting identities are retained.
* The native candidate is a separate clean Git worktree at
  `b519284bcedd9ad24c48fbc53219b818dde3f68e`, whose only difference from 24f is
  those four GC omissions in `go/pkg/driver.go`. It contains no probe source/API.
  Native baseline version is `v0.1.0-dev.next-four-base`; experimental native
  version is `v0.1.0-dev.ffi-gc-none`. Both actual library/CLI hashes and clean
  `vcs.revision`/`vcs.modified=false` build metadata were verified.

Go 1.27.3, macOS 26.7.3 arm64, Apple M5 Max/18 logical CPUs/36 GiB. Owned Redis
Software 8.2.6-25 (management API node version; container label 8.2.6),
Redis 8.8.0, Search 80808; one 4-GiB
noeviction RAM shard, active/effective 4x QPF, six Search workers, mnp/32
connections. Actual 4-CPU/8-GiB container limits, strict certificate/hostname
verified data TLS and RESP3 were rechecked before native execution. Management
uses the fixture bootstrap self-signed trust mode. The shared container image
was fixed/recorded privately, but its tag was not digest-pinned. Python/package
versions remain in native observations. Other builds/tests/timing were paused;
light source/document work continued. Operational paths/identifiers stay private.

## Failed attempts and remaining gates

Retained negative attempts include a failed virtual-cgo overlay build (missing
generated C files), an initial candidate compile failure from removing `runtime`
while `runtime.Caller` still needed it, two controller name-resolution mistakes
before subprocess execution, and both race/checkptr SIGABRTs (exit -6). The
corrected patch retains the runtime import and every non-GC runtime use. No
measurement existed before the compile correction; no failed timing was retried.

Both race builds succeeded, then both executions aborted in the existing
integer-handle-to-pointer conversion before the ownership scenarios. This is a
blocked race gate, not a passing race test or evidence that GC omission caused
the abort. Checkptr remains enabled. No ASAN run or production ownership-suite
result is claimed. Default removal is rejected on performance evidence and the
unresolved safety gates.

The owned DB was returned empty/index-free after each block with clients/processes
closed. Parent then reverified effective QPF/TLS/RESP3 and empty state before final
teardown. The [cleanup proof](cleanup.json) records owned container/network removal,
preservation of the one preexisting container and 31 networks, and removal of
generated TLS/environment/settings/credentials. No actual secret matches required
raw-log redaction; all recorded raw digests remain unchanged. No ClickBench was run.

See the [reproducer and scope](../README.md) and
[change validation](../../../docs/sdlc/changes/ffi-release-gc-attribution/validation.md).
