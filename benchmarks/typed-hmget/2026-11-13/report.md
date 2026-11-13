# Actual nullable HMGET API evaluation — November 13, 2026

The experimental typed API plus driver adapter is **not adopted as a driver default**.
All four native cases took 5.8–8.7% more median elapsed time, despite 2.8–5.5%
lower maximum-worker RSS. The current-master isolated parser comparison improved,
but does not establish an end-to-end driver gain. Default driver code/dependency
remain unchanged. All 36 parser observations and 56 native trials are retained.

## Complete native result

| Readers | Batch ceiling | Baseline seconds | Typed seconds | Baseline RSS MiB | Typed RSS MiB |
|---:|---:|---:|---:|---:|---:|
| 1 | 512 | 2.215 | 2.350 | 251.4 | 244.2 |
| 1 | 4096 | 2.204 | 2.332 | 252.0 | 245.0 |
| 4 | 512 | 1.449 | 1.576 | 253.8 | 241.7 |
| 4 | 4096 | 1.393 | 1.482 | 259.4 | 245.2 |

Baseline is clean driver `5f759300` with stock go-redis v9.23.0, version
`v0.1.0-dev.merged-perf-2`. Candidate is clean driver **base** `3213aa54` plus
the exact unapplied Go adapter overlay, private modfile/sum and patched go-redis
`30e29d1` based on v9.23.0 (`6f2b263`). Its version is
`v0.1.0-dev.typed-hmget`. The base VCS revision alone does **not** identify the
candidate implementation. All overlay/API source hashes, patches and module
hashes are retained in [results.json](results.json). Default go.mod is unchanged.

The adapter uses the actual owned nullable-string command and passes string/
validity vectors directly to positional projection without generic interface
reboxing. It also preboxes field names once per page rather than once per chunk
and adds an explicit per-chunk cancellation check. Therefore this evaluates the
complete typed API plus adapter; it does not isolate RESP decoding alone.

The unchanged managed_read_memory.py harness ran 100K rows × 105 columns with
one/four readers, 512/4096 batch ceilings and six measured repetitions after one
warmup per case. All 56 complete outputs passed independent physical-type,
value, count and multiplicity validation. Four readers execute eight independent
SQL ranges over fixed data, not shared live-partition snapshots. No failed,
retried, restarted or discarded measurement attempt; no retuning followed.

Baseline whole block ran before candidate; case order alternated/reversed within
each block. Blocks were not interleaved/randomized. Other chat heavy work paused,
but this is a shared workstation. Small samples/order/thermal effects limit
inference. Elapsed includes process/import startup, schema loading, complete
fetch/conversion, IPC output and shutdown; independent validation is untimed.
RSS is the maximum individual worker high-water mark, including Python, libraries,
source loading, Go/native buffers and IPC. It is not isolated Go heap or summed
process-tree memory. Every first-batch value and native sample remains retained.

Go 1.27.3, Apple M5 Max/18 logical CPUs/36 GiB, macOS arm64. Both native variants
leave GOMAXPROCS unset. Python 3.15.2, PyArrow 26.0.1, ADBC manager 1.13.0,
redis-py 8.1.0/hiredis 3.5.0. Owned Redis Software 8.2.6 / Redis 8.8.0 / Search
80808: verified active/effective 4x QPF, six Search workers, mnp/32 connections,
one 4-GiB noeviction RAM shard, four-CPU/eight-GiB container limits and strict
certificate/hostname-verified data TLS/RESP3. QPF checked before every case and
at each block end. No ClickBench or alternative server/provider was used.

## Isolated actual-API parser result

This separate comparison uses current-master API proposal `1f0ca9a` (base
`464d41a`), not the native v9.23.0 backport. Go 1.27.3, GOMAXPROCS=1, count=3,
200ms minimum per sample; actual SliceCmd and NullableStringSliceCmd readReply
plus full field consumption. All twelve cases × three observations completed.

| Rows/page | Buffer bytes | Generic ms | Nullable ms | Generic bytes | Nullable bytes | Generic allocations | Nullable allocations |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 256 | 32768 | 1.000 | 0.802 | 1124568 | 787016 | 48443 | 25810 |
| 256 | 1048576 | 0.945 | 0.774 | 1124568 | 787016 | 48443 | 25810 |
| 1024 | 32768 | 4.080 | 3.278 | 4668304 | 3318064 | 193777 | 103243 |
| 1024 | 1048576 | 4.034 | 3.189 | 4668304 | 3318064 | 193777 | 103243 |
| 4096 | 32768 | 16.738 | 13.204 | 18985960 | 13585032 | 775107 | 412973 |
| 4096 | 1048576 | 16.648 | 13.144 | 18985960 | 13585032 | 775107 | 412973 |

Across these six pairs, median parser/consumption elapsed fell 18.2–21.1%,
allocation bytes 28.4–30.0%, and allocations 46.7%. These are short local
observations, not confidence intervals or promised driver/RSS improvements.
Wire bytes, parser/command/args and outer-page scaffolding are outside timing;
fresh decoded vectors and owned payload strings plus every-field consumption
are inside. Complete byte/NULL validation runs before and after each case,
covering NULL versus empty, exact values above 2^53 and NUL/Unicode/CRLF.
Go benchmark calibration/order is fixed rather than randomized. Network/TLS,
Redis, pools/hooks, routing, Arrow, IPC and Python are excluded. No Redis server
is used by this parser-only measurement. Independent ownership/error/cache and
Software API checks are separate correctness evidence.

## Correctness, retained failed attempts and scope

Pinned API: five race-enabled tests passed, no skips/failures. Current-master
proposal: five passed, no skips/failures; upstream custom vet ./... passed.
Tests cover owned results/cache isolation, NULL/empty/binary/large values,
malformed/error replies, pipelines and actual Software clients/auto-pipeline
modes. Driver overlay checks passed 43 events with one intentional timing-
diagnostic opt-in skip, including defaults/NULL parity, retained Arrow,
snapshot UPDATE/DELETE/TRUNCATE/DROP after discovery, cancellation and release.
No independent Cluster/CSC-invalidation or complete upstream-suite claim is made.

The first Software API race attempt had three pass events and one failure:
its full-duplex test hook read results before next, violating the documented
auto-pipeline contract because the wire operation may already be queued. Only
the test hook changed: pre/post inspection remains for plain/half duplex;
full duplex inspects after next. The API implementation was unchanged. The
corrected four-test run passed; adding cache ownership coverage yielded five.
All raw failed/corrected attempts remain private with digests in results.json.

The first current-master compilation failed in context_deadline_test.go:45
because an existing mock callback lacked the new variadic onInit parameter.
The same failure was reproduced on clean pre-feature master464d41a. A test-only
mock signature repair retained its return value/assertions. That existing RESP2
fixture was compiled only, not executed; all actual Software connections used
RESP3. This pre-existing build repair is disclosed in the upstream patch.

Upstream proposal: [redis/go-redis#4308](https://github.com/redis/go-redis/pull/4308).
It remains an API proposal; the native result gives no reason to change the
driver dependency/default now. A server-internal or exact causal explanation
for the native regression has not been established.

## Reproduction and cleanup

Run `python3 benchmarks/typed-hmget/audit.py` to validate all 36+56 observations,
exact case/warmup counts, compound provenance, patch checksums, QPF and arithmetic.
The [runner](../run.py) uses fresh private worktrees and the supplied unapplied
patches; see [instructions](../README.md). Existing sources/default dependencies
are not modified. New builds have different path/commit/build hashes; verify
source/patch identities and record their actual hashes instead of relabeling.

The loaned database was returned empty/no-index, all clients/processes exited.
After remaining tracks completed, the parent reverified effective settings,
strict TLS/RESP3 and the empty DB, then removed only the owned container/network.
All one pre-existing container and 31 networks were preserved. Generated TLS,
environment, settings and fixture credential files were removed. The sanitized
[cleanup proof](cleanup.json) is retained. Credential-redaction inspection changed
no retained raw logs, so their recorded digests remain valid. Raw logs,
operational identities and data remain private.
