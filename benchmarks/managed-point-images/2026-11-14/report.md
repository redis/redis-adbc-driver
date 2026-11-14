# Managed point image retrieval diagnostics — November 14, 2026

These are **instrumented Go SQL diagnostics**, not shared-library/Python/ADBC
benchmark results or Redis server CPU. The [baseline](baseline.json) and
[candidate](candidate.json) retain every one of 96 operation observations per
build, detailed client call categories and threshold-selected slowlog observations.
All mutations returned one affected row; complete final value/count states and four-column schema width
were checked. The same 1K-row/four-public-column fixture and known internal row IDs
match the native scenario's shape; one additional ordinal control field is kept.
Diagnostic row labels use `row-{id}` rather than the native harness's zero-padded
labels. This is a workload-shape comparison, not an identical native input run.

| Operation | Baseline p50 ms | Candidate p50 ms | Baseline/candidate | Client exchanges, baseline → candidate |
|---|---:|---:|---:|---:|
| insert | 39.934 | 45.245 | 0.88× | 1443 → 1426 |
| update | 64.733 | 44.743 | 1.45× | 2214 → 1663 |
| delete | 49.587 | 42.910 | 1.16× | 1814 → 1527 |

The candidate fuses a one-key writer-scope HASH read and its conditional absence
proof into one bounded read-only script per segment. It still stops at the newest
image or tombstone, reads exactly the requested fields, and preserves all outer
pin/ownership/publication checks. Strict readers and multi-key batches retain the
prior path. Definite top-level script ACL refusal retains native-command fallback;
inner data failures and unknown replies fail without replay.

UPDATE's baseline made 192 HMGET and 192 HGETALL calls plus 320 individual ZSCORE
probes. The candidate made 416 `point_image_read` script calls instead; HASH and
conditional directory commands execute *inside* those scripts. This is a reduction
in sequential client exchanges, **not elimination of server HASH reads**.
Background compaction changes the observed number of segments between runs.

INSERT has no point-image read and is **slower in this diagnostic**. No causal
claim follows from these finite sequential samples. Concurrent compilation,
maintenance scheduling, allocator/GC and host load can affect them. Use the
coordinator's isolated full native paired matrices for end-to-end conclusions.

The baseline sustained and small helper profiles found fresh indexes ready on
the first poll, with 1.77–2.60 ms create-to-readiness. Faster polling was therefore
not implemented. Client-side admission waiting remains substantial; native
slowlog was available but its threshold-selected events are not correlated per
operation and do not establish Lua CPU or queueing costs. No unsupported server
CPU attribution or universal throughput/capacity claim is made.

[Provenance](provenance.json) pins production and diagnostic source identities and
all JSON input hashes. Both builds used verified Redis Software with active and
effective 4x QPF, six Search workers, mnp/32, noeviction and strict data TLS/RESP3.
Production maintenance remained enabled on its separate unhooked client. This
agent used the parent-owned fixture in an exclusive live slot; all its clients
closed before the slot returned. Infrastructure/credential identities and raw
operational logs remain private. No 100M ClickBench run was performed.

Verify retained samples, coverage, hashes and comparison arithmetic offline with
`python3 benchmarks/managed-point-images/audit.py`. This never contacts Redis.
The opt-in `TestManagedKnownRowWriteProfile` in `go/snapshot_point_write_profile_test.go`
regenerates the raw diagnostic payload on a newly verified owned fixture when
`REDIS_MANAGED_POINT_PROFILE=1` and the disposable snapshot URI are configured.
