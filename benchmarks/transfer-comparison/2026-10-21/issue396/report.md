# Issue #396: wide-result transfer investigation

> Historical investigation, published October 22, 2026. Recommendations and
> issue-state statements below describe the original investigation. See the
> [archive notes](../../README.md) for newer results and the archived profiler.

Measured 2026-10-21 on main commit `20de26967b9974fa344f15de2b71491039321d86`, Go 1.27.2, native macOS arm64. This is an investigation, with no production execution/default changes. The original historical wheel measurements are preserved separately.

## Findings

1. **The performance gap reproduces on a source-identified main build.** With identical typed Arrow IPC output, default ADBC takes 5.374 s for one reader and 3.121 s for four readers/eight ranges. The same-session known-key redis-py reference takes 3.216 s and 1.702 s: ADBC takes 1.67× / 1.83× as long. Native key reads still bypass generic SQL semantics.
2. **Intermediate row materialization is a strong optimization target.** The diagnostic allocation profile attributes 33.9% of allocated bytes to `decodeRow` and 26.9% to `evalItems` (60.8% together across both stage strategies). Plain `LOAD *` allocates about 5.87 GB / 53.3 million allocations for this 79.2 MB logical Arrow result. These are cumulative allocations, not peak RSS. A specialized simple-column projection should avoid constructing both a typed name map and another full row of `Value`s. Keep the generic path for expressions/residual predicates; preserve missing/default/null, exact integer and cancellation semantics.
3. **Do not switch every wide scan to discovery + HMGET.** That diagnostic cuts incoming RESP bytes by 56.1% (330.6 MB → 145.3 MB), but increases total time by 7.4% (4.764 s → 5.119 s) and allocated bytes by 6.0% (5.87 GB → 6.22 GB). Repeated maps and pipeline command construction remain. Less transport does not establish better end-to-end latency on localhost. It could be worth retesting after reducing allocations or with different bandwidth/latency.
4. **A smaller existing batch setting helps memory and first results.** At 2,048 rows, single-reader peak worker RSS falls 85.5% and time to first batch falls 87.7%, with 3.3% greater median total time. Four-reader gains are smaller because each of eight partitions contains only 12,500 rows. This supports an opt-in tuning recommendation and a future width/byte-aware batch budget; it does not justify a universal smaller default or a throughput claim.

## APIs tested

One reader uses regular `cursor.execute()` + `fetch_record_batch()`. Four readers use `adbc_execute_partitions()` with partition count eight, then four fresh workers call `adbc_read_partition()` + `fetch_record_batch()`. Partition planning and worker startup are timed. The Go stage diagnostic bypasses these APIs and measures scan helpers directly.

## End-to-end settings sweep

Medians of three measured trials after one warm-up for each method/profile. Every one of the 32 executions passed exact validation of all 100,000 × 105 values and multiplicities against the independently typed source. Method order rotates. Timing includes fresh spawned workers/imports, connections, partition planning, complete reads, normalization, Arrow IPC output and teardown; loading/validation excluded.

| Method | Readers | Stream batch rows | Total seconds | First batch seconds | Peak worker RSS MiB |
|---|---:|---:|---:|---:|---:|
| ADBC | 1 | 65,536 | 5.374 | 3.629 | 2,163 |
| ADBC | 1 | 8,192 | 5.581 | 0.814 | 475 |
| ADBC | 1 | 2,048 | 5.551 | 0.445 | 314 |
| ADBC | 4 | 65,536 | 3.121 | 1.555 | 504 |
| ADBC | 4 | 8,192 | 3.142 | 1.269 | 509 |
| ADBC | 4 | 2,048 | 3.176 | 0.591 | 308 |
| redis-py, known keys | 1 | 1,000-command pipeline | 3.216 | 0.198 | 114 |
| redis-py, known keys | 4 | 1,000-command pipeline | 1.702 | 0.234 | 112 |

RSS is the median of each trial's maximum worker lifetime high-water mark, including imports. It is not simultaneous aggregate memory across four workers or Redis server memory. The 2 GiB container limit applies to Redis, not host clients. First batch includes startup/planning and depends on the method's output batch size. Raw ranges are retained in `sweep-results.json`; overlapping timing ranges mean small throughput differences should not be overinterpreted.

The setting is `adbc.redis.stream_batch_rows` (database or statement). For the Python connection used here:

```python
adbc_driver_manager.dbapi.connect(
    driver=driver_path,
    db_kwargs={"uri": redis_uri, "adbc.redis.stream_batch_rows": "2048"},
    autocommit=True,
)
```

## Diagnostic stage experiment

Single process with an already open connection, 1,869-row Search pages, no process startup, and Arrow output written every page. Uses production decoding/projection/Arrow helpers with Go's default allocator. This isolates stages rather than reproducing the complete ADBC reader: it bypasses readiness/generation checks and uses the immutable, managed, non-null fixture; it does not preserve all general SQL/live-read semantics. The native library uses a C malloc allocator instead of this Go Arrow allocator. **Do not subtract these timings from end-to-end timings.**

| Stage | LOAD * ms | Discovery + HMGET ms |
|---|---:|---:|
| Search commands, RESP decode and raw maps | 2,791 | 227 |
| HMGET commands, RESP decode, maps and merge | 0 | 2,947 |
| Typed row decoding (`keep`/`decodeRow`) | 969 | 976 |
| Projection and row fitting | 414 | 432 |
| Arrow record construction | 488 | 490 |
| IPC writes / record release | 60 | 60 |
| Total | 4,764 | 5,119 |

Values are independent per-stage medians, so they need not sum to the median total. Search handling is approximately 59% of LOAD * time, typed decode/projection approximately 29%, and Arrow construction approximately 10%. Search timing includes client protocol parsing/map creation as well as server execution and socket waits; it is not a server-only attribution. Initial cursor page is included. Incoming bytes count socket reads, not logical Arrow bytes or outgoing commands.

Eight stage executions returned the expected row count; the final Arrow output of each strategy was independently compared against every source value and multiplicity. CPU and sampled allocation profiles, stage logs and summaries are retained. Allocation hot spots are actionable evidence; macOS CPU samples contain substantial runtime/OS activity and do not establish a clean server/client CPU split.

## Native baseline with Search discovery

A separate follow-up rotates known-key and Search-discovery native methods, with one warm-up and three measured trials for each reader profile. All 16 executions passed exact full-value/multiplicity validation.

| Native redis-py method | One reader seconds | Four readers seconds |
|---|---:|---:|
| Known-key HMGET | 3.163 | 1.665 |
| FT.AGGREGATE key/row-ID discovery + HMGET | 3.759 | 1.964 |

The discovery baseline uses 1,869-row Search cursor pages, the same 105 output fields, bounded 1,000-command HMGET pipelines, an extra `__rowid` existence field, and exact Arrow conversion/IPC. Four readers use eight 12,500-row ID ranges. It closes live cursors on exit. It still omits SQL planning, catalog/generation/readiness validation and general missing/default/null handling. The immutable dense fixture makes those omissions safe for this experiment, not for a general driver implementation.

Discovery has a measurable cost, but these native times remain below the earlier source-identified ADBC runs (5.374/3.121 s). The native-discovery follow-up and ADBC sweep ran sequentially, not interleaved, so do not treat subtraction as an exact attribution or those cross-experiment ratios as a new paired speedup. The gap is not explained solely by the known-key shortcut.

## Recommended next implementation

- Prototype a guarded simple-projection fast path using prebound column slots and reusable bounded buffers/direct Arrow builders. Avoid the typed map plus projected `Value` row duplication. Preserve generic execution for other shapes.
- Add a byte/width-aware upper bound to pending stream rows. Measure narrow queries before changing defaults. Existing 2,048-row batches are a practical memory/first-batch option for this wide fixture.
- Optimize raw Search reply parsing after measuring allocations separately from server work. Evaluate direct field-slot decoding and avoiding transient generic reply/map copies before adding a second retrieval stage.
- Keep wide HMGET as an experiment until it wins measured latency/CPU/memory, not just incoming bytes.

Before shipping, validate narrow/wide shapes, sparse/deleted IDs, empty results, defaults/null markers, integers beyond 2^53, timestamps, NUL strings, residual predicates, cancellation, metadata/readiness changes, standalone and Cluster. No production change was made in this investigation, so that regression matrix has not been claimed as completed. Issue #396 should remain open.

## Evidence and identity

`manifest.json` records exact source commit, library/test binary SHA-256, Go version, source Arrow hash, package/server versions, and container settings. Source was exported separately from latest main; the working checkout remains at `9f5188c411f1e3a3bf1423a72089e2441b7995bd`. Main's changes since that checkout concern readiness/cleanup; the ordinary wide scan and Arrow construction paths are unchanged. The fixture was loaded with the historical wheel, then read with the source-identified main library and Go test binary. No data mutation occurs during measured runs.

Host, fixture, logical payload and native client versions match the parent benchmark report. Local Redis 8.6.2 / Search 8.6.0, four server CPUs/2 GiB, localhost TCP, no TLS; 100,000 rows × 105 columns and 79,228,019 logical Arrow bytes. These results do not establish WAN, production, 100-million-row, or generic SQL product rankings.

## Reproduction

Retained scripts require the Python dependencies and original ClickBench DDL/parquet fixture described in the parent report. The source-prefix parquet SHA-256 and package versions are in the manifest. Use a fresh dedicated Redis server; do not run fixture loading against an existing application database.

From the repository root, with `PARQUET` pointing to the verified 100K prefix and `CLICKBENCH` to the upstream ClickBench checkout:

```sh
export REDIS_TRANSFER_REPO_ROOT="$PWD"
export PROFILE_WORK=/private/tmp/redis-issue396-repro
mkdir -p "$PROFILE_WORK/source"
git archive 20de26967b9974fa344f15de2b71491039321d86 | tar -x -C "$PROFILE_WORK/source"
cp benchmarks/transfer-comparison/2026-10-21/issue396/wide_transfer_profile_test.go "$PROFILE_WORK/source/go/"
cp benchmarks/transfer-comparison/2026-10-21/issue396/*.py "$PROFILE_WORK/"
docker run -d --name redis-issue396-repro --cpus 4 --memory 2g -p 127.0.0.1:17979:6379 redis:8.6.2
(
  cd "$PROFILE_WORK/source/go"
  go test -c -o "$PROFILE_WORK/main.test" .
  CGO_ENABLED=1 go build -buildmode=c-shared -tags driverlib -o "$PROFILE_WORK/main.dylib" ./pkg
)
python "$PROFILE_WORK/benchmark.py" --driver "$PROFILE_WORK/main.dylib" --parquet "$PARQUET" --upstream "$CLICKBENCH" --work "$PROFILE_WORK" --phase load --engines redis
REDIS_URI=redis://127.0.0.1:17979/0 REDIS_WIDE_PROFILE_DIR="$PROFILE_WORK/stages" "$PROFILE_WORK/main.test" -test.run '^TestWideTransferProfile$' -test.v -test.cpuprofile "$PROFILE_WORK/cpu.pprof" -test.memprofile "$PROFILE_WORK/alloc.pprof"
python "$PROFILE_WORK/validate_stages.py" "$PROFILE_WORK"
python "$PROFILE_WORK/sweep.py"
python "$PROFILE_WORK/discovery.py"
go tool pprof -top -alloc_space "$PROFILE_WORK/main.test" "$PROFILE_WORK/alloc.pprof"
docker rm -f redis-issue396-repro
```

The `.dylib` build command is the measured macOS configuration; use the platform's shared-library suffix on other hosts and update the runner paths. Preserve the raw outputs and identities for a new run. Main stage final outputs were independently validated after casting to the source Arrow schema and sorting on all 105 columns; the end-to-end runners perform this validation automatically for every execution. Initial checkout-stage diagnostics were superseded after removing a profiling-harness-only repeated column-schema allocation; only the corrected main results are retained here.
