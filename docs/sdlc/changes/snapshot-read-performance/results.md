# Snapshot read performance evidence

Date: 2026-11-06. Issue [#338](https://github.com/redis/redis-adbc-driver/issues/338), PR [#349](https://github.com/redis/redis-adbc-driver/pull/349). Requestor: repository owner; author: Codex. Implementation and comparative evidence complete; final-head delivery status is recorded on the PR.

## Implementation and source identities

The final change reuses page-scoped scratch in selective candidate scans only. Ordinary directory scanning is byte-for-byte identical to the reference. A prototype changed ordinary directory scratch lifetime and removed padded `fmt.Sprintf` validation; the combined prototype was rejected after complete native read regressions. The final candidate retains the original canonical validation. Head validation, every source read, newest-winner selection, advancement of equal heads, row hydration and all server checks retain their order. No command batching, masks, transport protocol, format, grants, page bounds or lifetime rules change.

Clean native libraries were built with Go 1.27.3 and the Makefile driver version/linker flags:

- Mutable: `f4d5b787e8c28c036baf49de9190c4e6f83ca2e8` (historical mutable storage).
- Reference: `1970bf4a49d13f70f764bf647caa6dad15dc8018` (protocol 16).
- Rejected formatting prototype: `3cc906dca59ad205a303ead99ffaa8e1799f582c` (protocol 16).
- Intermediate scratch-only ordinary-scanner experiment: `b514a52a` (protocol 16), built but not selected for delivery.
- Candidate-scan-only runtime: `cdd75468eb207dc0968356a1e2e4fa5113f4f328`; the clean evidence/native-build commit is `528ef2dd07b904eb239d153d7c246cd94dfafacf`. Ordinary scans are restored to the reference.

The native JSON retains actual library and harness SHA-256s. Each process loads one library and resets only its owned disposable fixture. All complete Arrow results are independently checked. Each case has one warmup plus five recorded samples. Narrow cases include 32 unrelated tables. Wide cases have 64 columns. Exact cases include BIGINT above 2^53, decimal(24,4), NULL, arbitrary binary bytes, replacements/deletions, filters, ordering/limits and four live partitions. The partition timing includes production, all worker fetches, concatenation/sorting and validation; explicit group release follows the timer. It does not measure distributed workers on different hosts. Existing native snapshot validation separately tests visibility through concurrent writes, producer Close and recreation.

## Environment and limits

Redis Software 8.2.6-25, Redis 8.8.0, active/effective 4x QPF verified through management API, strict authenticated TLS, noeviction, three primary shards behind the local proxy. Software image digest `sha256:b3993a5ed453ffe2aeacff63db7dd3702253b7e2f732032676e2b221413e0e06`. macOS 26.7.2 arm64, Apple M5 Max, 18 logical CPUs, 36 GiB host memory; Docker Desktop: 18 CPUs, 25,420,967,936 memory bytes. Python/Arrow/client versions and hashes are retained in the evidence metadata. This is synthetic owned-fixture testing, not ClickBench.

Managed row versions share the metadata hash slot; mutable rows can distribute across the three shards. This compares actual storage models, not identical per-shard engine cost. Initial native series leave maintenance enabled without sampling topology. A separate controlled series waits for both the compaction queue and active job to empty, then records per-case immutable segment row counts outside query timers. Results from different harness revisions/conditions are reported separately. Named local build/test processes were checked before/during timing; this is not a guarantee that the whole host was idle. Resource attribution uses a separate controlled Go fixture with background workers disabled.

## Upstream capability review

On 2026-11-06, [Redis #16048](https://github.com/redis/redis/issues/16048) (bounded typed bulk export) and [Search #12219](https://github.com/RediSearch/RediSearch/issues/12219) (reusable point-in-time read views) were both open feature requests. No supported typed Arrow export or reusable Search read-view interface was established for this Software fixture. Their proposals are not product capability evidence. The existing bounded HASH projection ([HMGET command](https://redis.io/docs/latest/commands/hmget/)), exact directories and visibility-safe Search/fallback paths remain in use. No speculative command/capability name, new transfer protocol or mask machinery was introduced.

The older EVAL_RO wrapper reduced request bytes but worsened full read latency; see the retained [historical comparison](../snapshot-parity/reader-directory-cache-validation.md) and upstream #16048. RDB investigation [#345](https://github.com/redis/redis-adbc-driver/issues/345) remains separate and unresolved; this change makes no recovery-fix claim.

## Final candidate measurements

The final native library is clean source `528ef2dd07b904eb239d153d7c246cd94dfafacf` (Go 1.27.3, `vcs.modified=false`). Its production change is only the candidate scanner scratch lifetime; ordinary `snapshot_delta.go` is identical to the reference. Later commits add only tests/evidence. [All final native samples and binary/harness hashes](native-samples.json), [all Go resource samples](resource-samples.json), [all candidate merge samples](candidate-merge-samples.json) and [rejected prototype samples](prototype-native-samples.json) are retained separately. Do not pool series A/B/C or their harness revisions with the final series.

Client candidate-merge microbenchmark, median of three one-second trials; all 256 logical/physical winners are checked. Fake score-zero membership replies remove network wait. This quantifies local merge work, not Redis speed:

| Sources | Reference ns/page | Final ns/page | Reference bytes / allocs | Final bytes / allocs |
|---|---:|---:|---:|---:|
| 1 | 19,769 | 20,303 | 42,208 / 699 | 42,208 / 699 |
| 4 | 39,628 | 37,031 | 114,024 / 1,762 | 97,704 / 1,507 |
| 8 | 62,900 | 59,558 | 204,360 / 2,836 | 171,720 / 2,581 |

With four/eight sources the changed path saves 255 allocations per full 256-candidate page. One source already avoids the per-row heap allocation and shows no allocation reduction. The complete 101-row Go filter profile has median 13,561 allocations in the reference and 13,482 in the final candidate; both read 59,328 RESP bytes. Harness/control allocations vary, so the isolated benchmark supplies the deterministic attribution. Full ordinary scans, aggregates, ordered readers and partitions keep their original implementation; fluctuations in their measurements are not attributed to candidate scratch reuse.

Native elapsed milliseconds below are the median of the two process medians for each source (each process has five samples, plus a warmup). The two process medians are the units of replication; samples are not pooled to hide process variance. Every native case has a complete independent result check. Final normal/exact snapshots have three immutable physical segments `[20000,1,1]`; wide snapshots have `[20000,5000,2857]` (including replacements/tombstones). Both have overlay enabled and empty compaction queue/job at the per-case inspection; mutable storage has no snapshot segments. Maintenance remains enabled. No native SQL/input retries were added.

| Mode / case | Mutable ms | Reference ms | Final ms | Final vs reference |
|---|---:|---:|---:|---:|
| normal / implicit_all | 117.616 | 510.083 | 426.961 | -16.30% |
| normal / selective | 6.131 | 14.824 | 13.939 | -5.97% |
| normal / matching_selective | 3.758 | 12.668 | 12.350 | -2.51% |
| normal / grouped | 4.446 | 13.317 | 13.299 | -0.14% |
| normal / distinct | 4.439 | 13.533 | 13.617 | +0.62% |
| normal / topk | 76.417 | 15.759 | 15.666 | -0.59% |
| normal / topk_nulls_last | 4.230 | 12.593 | 12.657 | +0.51% |
| normal / point | 5.163 | 9.437 | 9.565 | +1.35% |
| wide / wide_directory | 656.902 | 1548.771 | 1577.766 | +1.87% |
| wide / wide_candidates | 160.908 | 69.741 | 70.623 | +1.27% |
| exact / typed_all | 118.281 | 737.535 | 537.025 | -27.19% |
| exact / typed_filter | 24.786 | 27.188 | 25.287 | -6.99% |
| exact / typed_limit | 81.099 | 17.070 | 19.580 | +14.70% |
| exact / typed_partitions | 166.390 | 589.814 | 658.032 | +11.57% |

Negative cases are retained: wide directory/candidates, distinct, point, explicit NULL ordering, typed ordering/limit and typed partitions are slower in this final comparison. Typed limit is +14.70% and typed partitions +11.57%; the raw samples show substantial variability (including a 991.84 ms final partition sample). These unchanged ordered/partition paths do not establish a gain from scratch reuse. Likewise, the lower full-scan medians do not establish a causal speedup: ordinary scan code is unchanged. No blanket query-latency improvement or nonregression guarantee is claimed from this small single-host experiment.

**Full mutable performance parity is not achieved.** Final full ordinary/typed reads and partitions remain substantially slower than mutable storage. Some indexed candidate/order shapes are faster than mutable. The upstream bulk export/read-view requests remain open; they are separate product work, not newly implemented capabilities. This closes #338's bounded profiling, safe local allocation optimization, comparative evidence and capability-evaluation acceptance, not the broader upstream latency gap.

## Validation and delivery gates

- Unit race suite before prototype rejection: 516 top-level passes, 766 endpoint/optional skips; six packages passed, three had no applicable tests. This is unit evidence, not Software compatibility proof.
- First full Software race attempt failed two initialization tests after the mutable resource fixture was left populated. Correct fresh-storage refusal; retained privately and not reported as a pass.
- Complete Software 4x QPF race rerun from an empty owned fixture: 263 top-level passes, 39 skips, zero failures. This run exercised the earlier combined prototype, including the exact candidate scanner implementation retained in the final source; it is not mislabeled as final ordinary-scanner proof.
- Narrowed final source: real Software RBAC `TestSQLRBACSelect` passed under the race detector; native snapshot validation 19/19 passed; official native ADBC validation 565 tests plus three subtests passed, eight explicit unsupported-capability skips.
- Final-head unit/full Software race results and selected Test/Release CI checks are delivery gates recorded on [PR #349](https://github.com/redis/redis-adbc-driver/pull/349). Merge requires their completed results, not this earlier source's green checks. No release tag or package publication is requested.

Review: Codex automated correctness/lifecycle, security/ownership and intent/evidence passes. The final runtime only moves the candidate scratch allocation outside the row loop. Each slot is overwritten on every successful head pass; errors return before stale scratch can be consumed. Equal-head advancement, newer exact-directory probes, missing-newest-HASH checks, EOF verification, hydration, tombstones, masks, bounds, cursor/cancellation and worker behavior retain their order. No permission/storage compatibility change is present. The ordinary-scanner prototype was rejected rather than accepted on allocation counts. This review is not a human maintainer approval. Current repository rules and final-head mergeability must be checked at delivery.

## Reproduction and review

`go/validation/snapshot_read_benchmark.py` requires the owned-reset flag and verified Software API/data settings. Run one revision per process, with `--rows 20000 --repeats 5`, using normal, `--wide` and `--exact` modes; use mutable/reference/current then current/reference/mutable order. Never supply a shared database. Setup is outside timers; complete fetch and validation are inside.

`BenchmarkSnapshotDirectoryMerge` isolates preloaded immutable pages at one/four/eight sources, with overlapping IDs and gaps, and precomputes the independent newest-winner oracle outside the timer. `TestSnapshotReadResourceProfile`, enabled with `REDIS_SNAPSHOT_READ_PROFILE=1` on verified Software, consumes all Arrow rows for narrow/wide/filter/decimal aggregate/order-limit/live-partition cases. MemStats and client CPU cover the process, including Go harness materialization. Connection bytes count RESP traffic including control operations, excluding TLS framing. Server CPU is not measured. Reference/current use the identical harness with unchanged respective production trees. For the historical mutable checkout, apply [the test-only adapter](mutable-profile-adapter.patch) after copying the profiling file: it removes the snapshot-only merge benchmark, uses the historical fixture factory, omits absent snapshot group lifecycle APIs and supplies the same process CPU calculation. Queries, data, oracle and instrumentation are unchanged.

Retained pilots include an interrupted first microbenchmark whose oracle allocated inside the timer; early overlapping build/test attribution; the exact harness capability pilots; and an expanded profile whose BIGINT SUM correctly returned 22003. Final resource profiles use an explicit decimal SUM and isolated runs. These pilots are not pooled with final evidence. The first full snapshot race run also failed two initialization tests because the historical mutable resource fixture had been left populated; the driver correctly refused to adopt that storage. That failed run is retained separately. The complete suite was rerun successfully after an explicit owned-database reset. Operational logs, endpoint/credential data, management artifacts and licenses remain private.
