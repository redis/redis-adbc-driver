# Snapshot read performance evidence

Date: 2026-11-06. Issue [#338](https://github.com/redis/redis-adbc-driver/issues/338), PR [#349](https://github.com/redis/redis-adbc-driver/pull/349). Requestor: repository owner; author: Codex. Validation in progress.

## Implementation and source identities

Page-scoped merge scratch replaces a separate dynamic scratch slice for each row in directory and candidate scans. Directory scans compare the canonical parsed positive int64 against the 20-byte entry with only zero padding removed, avoiding padded `fmt.Sprintf` per row. Head validation, every source read, newest-winner selection, advancement of equal heads, row hydration and all server checks retain their order. No command batching, masks, transport protocol, format, grants, page bounds or lifetime rules change.

Clean native libraries were built with Go 1.27.3 and the Makefile driver version/linker flags:

- Mutable: `f4d5b787e8c28c036baf49de9190c4e6f83ca2e8` (historical mutable storage).
- Reference: `1970bf4a49d13f70f764bf647caa6dad15dc8018` (protocol 16).
- Current runtime: `3cc906dca59ad205a303ead99ffaa8e1799f582c` (protocol 16). Subsequent changes are profiling/tests/evidence only; verify production-tree equivalence before delivery.

The native JSON retains actual library and harness SHA-256s. Each process loads one library and resets only its owned disposable fixture. All complete Arrow results are independently checked. Each case has one warmup plus five recorded samples. Narrow cases include 32 unrelated tables. Wide cases have 64 columns. Exact cases include BIGINT above 2^53, decimal(24,4), NULL, arbitrary binary bytes, replacements/deletions, filters, ordering/limits and four live partitions. The partition timing includes production, all worker fetches, concatenation/sorting and validation; explicit group release follows the timer. It does not measure distributed workers on different hosts. Existing native snapshot validation separately tests visibility through concurrent writes, producer Close and recreation.

## Environment and limits

Redis Software 8.2.6-25, Redis 8.8.0, active/effective 4x QPF verified through management API, strict authenticated TLS, noeviction, three primary shards behind the local proxy. Software image digest `sha256:b3993a5ed453ffe2aeacff63db7dd3702253b7e2f732032676e2b221413e0e06`. macOS 26.7.2 arm64, Apple M5 Max, 18 logical CPUs, 36 GiB host memory; Docker Desktop: 18 CPUs, 25,420,967,936 memory bytes. Python/Arrow/client versions and hashes are retained in the evidence metadata. This is synthetic owned-fixture testing, not ClickBench.

Managed row versions share the metadata hash slot; mutable rows can distribute across the three shards. This compares actual storage models, not identical per-shard engine cost. Background maintenance is enabled during native runs and segment topology is not sampled, so these are end-to-end comparisons rather than a causal decomposition of the merge optimization. Named local build/test processes were checked before/during timing; this is not a guarantee that the whole host was idle. Resource attribution uses a separate controlled Go fixture with background workers disabled.

## Upstream capability review

On 2026-11-06, [Redis #16048](https://github.com/redis/redis/issues/16048) (bounded typed bulk export) and [Search #12219](https://github.com/RediSearch/RediSearch/issues/12219) (reusable point-in-time read views) were both open feature requests. No supported typed Arrow export or reusable Search read-view interface was established for this Software fixture. Their proposals are not product capability evidence. The existing bounded HASH projection ([HMGET command](https://redis.io/docs/latest/commands/hmget/)), exact directories and visibility-safe Search/fallback paths remain in use. No speculative command/capability name, new transfer protocol or mask machinery was introduced.

The older EVAL_RO wrapper reduced request bytes but worsened full read latency; see the retained [historical comparison](../snapshot-parity/reader-directory-cache-validation.md) and upstream #16048. RDB investigation [#345](https://github.com/redis/redis-adbc-driver/issues/345) remains separate and unresolved; this change makes no recovery-fix claim.

## Resource attribution and native latency

Final tables and complete samples will be attached after the balanced repeat and validation finish. Fewer allocations are not a claim of lower query latency, general speedup or mutable performance parity.

## Reproduction and review

`go/validation/snapshot_read_benchmark.py` requires the owned-reset flag and verified Software API/data settings. Run one revision per process, with `--rows 20000 --repeats 5`, using normal, `--wide` and `--exact` modes; use mutable/reference/current then current/reference/mutable order. Never supply a shared database. Setup is outside timers; complete fetch and validation are inside.

`BenchmarkSnapshotDirectoryMerge` isolates preloaded immutable pages at one/four/eight sources, with overlapping IDs and gaps, and precomputes the independent newest-winner oracle outside the timer. `TestSnapshotReadResourceProfile`, enabled with `REDIS_SNAPSHOT_READ_PROFILE=1` on verified Software, consumes all Arrow rows for narrow/wide/filter/decimal aggregate/order-limit/live-partition cases. MemStats and client CPU cover the process, including Go harness materialization. Connection bytes count RESP traffic including control operations, excluding TLS framing. Server CPU is not measured. Reference/current use the identical harness with unchanged respective production trees. For the historical mutable checkout, apply [the test-only adapter](mutable-profile-adapter.patch) after copying the profiling file: it removes the snapshot-only merge benchmark, uses the historical fixture factory, omits absent snapshot group lifecycle APIs and supplies the same process CPU calculation. Queries, data, oracle and instrumentation are unchanged.

Retained pilots include an interrupted first microbenchmark whose oracle allocated inside the timer; early overlapping build/test attribution; the exact harness capability pilots; and an expanded profile whose BIGINT SUM correctly returned 22003. Final resource profiles use an explicit decimal SUM and isolated runs. These pilots are not pooled with final evidence. The first full snapshot race run also failed two initialization tests because the historical mutable resource fixture had been left populated; the driver correctly refused to adopt that storage. That failed run is retained separately. The complete suite must be rerun after an explicit owned-database reset. Operational logs, endpoint/credential data, management artifacts and licenses remain private.
