# Incremental reads: Redis ADBC and official redis-py

Measured October 22, 2026. **100,000 rows × 105 columns**, full typed Arrow IPC.
This tests the exact **#422 wide-transfer comparison binary** (source-equivalent to
`7a42b2d`), not the later current-main build. No driver production code changed.

## Findings

- With one reader and 4,096-row output batches, ADBC completes in **2.584 s**, versus
  **3.122 s** for redis-py known keys and **3.364 s** for redis-py Search discovery.
  That is **17.2% / 23.2% less total elapsed time**. Full-transfer timing ranges are separated.
- ADBC can use the same application-managed range pattern as the native readers:
  eight explicit SQL row-ID ranges consumed by four independent processes.
  Its median full-transfer time falls from **1.778 s** with public partitions to
  **1.586 s** with explicit ranges, **10.8% less elapsed time**.
- Four-reader redis-py Search discovery takes **1.561 s**. Its range overlaps ADBC's
  explicit-range result, supporting comparable elapsed performance in this
  experiment, not a clear ADBC win. Known-key redis-py is **1.522 s**.
- **No ADBC time-to-first-batch advantage is demonstrated.** At 4,096 rows/batch,
  one-reader ADBC is **0.316 s** versus redis-py **0.287 / 0.306 s**;
  four-reader ADBC explicit ranges are **0.406 s** versus redis-py
  **0.386 / 0.341 s**. Some first-batch ranges overlap.
  Every method begins delivering well before the full result finishes.

## Matched measurements

Each cell shows **median (minimum–maximum)** of six measured executions.
First batch means a complete typed Arrow RecordBatch after common normalization,
measured in the producer process before IPC writing. It does not measure a
remote consumer or dashboard rendering. `results.json` separately records the
first IPC flush, normally about 1–2 ms later. Batch sizes match by row count;
traversal order and variable-length byte content may differ.

### 4,096-row output batches

| Method | Readers | First batch, s | Complete transfer, s |
|---|---:|---:|---:|
| ADBC statement | 1 | 0.316 (0.297–0.342) | 2.584 (2.537–2.681) |
| redis-py + hiredis, known keys | 1 | 0.287 (0.266–0.293) | 3.122 (3.082–3.159) |
| redis-py + hiredis, Search + HMGET | 1 | 0.306 (0.303–0.430) | 3.364 (3.274–3.672) |
| ADBC public partitions | 4 | 0.460 (0.433–0.508) | 1.778 (1.700–1.869) |
| ADBC explicit SQL ranges | 4 | 0.406 (0.372–0.505) | 1.586 (1.480–1.746) |
| redis-py + hiredis, known keys | 4 | 0.386 (0.361–0.410) | 1.522 (1.399–1.631) |
| redis-py + hiredis, Search + HMGET | 4 | 0.341 (0.328–0.352) | 1.561 (1.436–1.710) |

### 512-row output batches

| Method | Readers | First batch, s | Complete transfer, s |
|---|---:|---:|---:|
| ADBC statement | 1 | 0.326 (0.306–0.332) | 2.746 (2.731–2.839) |
| redis-py + hiredis, known keys | 1 | 0.269 (0.259–0.274) | 3.313 (3.177–3.474) |
| redis-py + hiredis, Search + HMGET | 1 | 0.280 (0.279–0.286) | 3.569 (3.457–3.780) |
| ADBC public partitions | 4 | 0.464 (0.432–0.524) | 1.848 (1.798–1.929) |
| ADBC explicit SQL ranges | 4 | 0.402 (0.391–0.446) | 1.655 (1.526–1.754) |
| redis-py + hiredis, known keys | 4 | 0.356 (0.332–0.373) | 1.625 (1.459–1.682) |
| redis-py + hiredis, Search + HMGET | 4 | 0.316 (0.305–0.369) | 1.647 (1.594–1.835) |

## What explains the four-reader gap?

Descriptor planning itself took a median **27.9 ms** at 4,096 rows/batch.
That is smaller than the **192.1 ms** difference between the public
partition and explicit-range medians. The paths also do different work during
reading; removing planning alone is not a sufficient explanation.

The command counters below were consistent across all six measured
4,096-row/four-reader trials. All paths perform exactly **100,000 HMGETs**.

| Method | FT.INFO | CONFIG GET | FT.AGGREGATE | FT.CURSOR READ |
|---|---:|---:|---:|---:|
| ADBC public partitions | 96 | 24 | 17 | 24 |
| ADBC explicit SQL ranges | 0 | 8 | 16 | 24 |
| redis-py + hiredis, known keys | 0 | 0 | 0 | 0 |
| redis-py + hiredis, Search + HMGET | 0 | 0 | 8 | 24 |

The public partition implementation verifies index readiness and runtime
Search limits on bounded reads. The ordinary explicit-range statement path
and fixture-specific native readers have different discovery/checking work.
These counters are consistent with extra partition-path overhead; they do not
isolate a causal cost per command. This is not an experiment disabling
correctness checks in the production partition API.

The optimization to investigate is reducing repeated setup/metadata work while
preserving the public API's completeness, readiness, lifecycle and cancellation
checks. Explicit ranges are already usable when the application owns the range
boundaries and data is immutable. This benchmark does not establish equivalent
behavior during concurrent table/index changes or a safe generic replacement
for automatic partitioning.

## Method and validation

- One warmup for each of 14 cases, then six measured rounds: **98 executions**
  (14 warmups + 84 measured), all with independent complete-value and row-
  multiplicity validation. A separate 14-case smoke run also passed and is not
  included in the measured summaries.
- Cases: two output batch sizes; one-reader ADBC / native known keys / native
  Search discovery; four-reader public partitions / explicit SQL ranges /
  native known keys / native Search discovery. Forward/reverse case order
  alternates for three measured rounds in each order.
- One reader scans all 100K rows. Four readers consume eight 12,500-row tasks.
  Public ADBC uses ExecutePartitions/ReadPartition. Explicit ADBC ranges issue
  `SELECT <105 columns> FROM transferbench.hits WHERE __rowid >= lo AND __rowid <= hi`.
  Native readers use the same dense ID bounds. Known keys omit discovery;
  the other native path uses FT.AGGREGATE WITHCURSOR then pipelined HMGET.
- Redis fetch bounds remain 4,096 rows. Native pipeline and Search page bounds
  are both 4,096. Native output is split and converted into 512/4,096-row Arrow
  batches; ADBC sets `adbc.redis.stream_batch_rows` to the same row ceiling.
  Thus the 512-row test changes the output ceiling while holding network
  retrieval bounds steady. It is not a sweep of every native pipeline size.
- First-batch and full-transfer clocks include worker spawn/imports, schema
  loading, connections, and public partition planning. Full transfer also
  includes all conversion, IPC output, connection close and process teardown.
  Exact-value sorting/comparison and source conversion/loading are outside timing.
- All outputs have 100,000 rows, 105 columns, and the same independently typed
  ClickBench prefix values. Native references use an immutable non-null fixture
  with known schema and omit general SQL/catalog/readiness semantics.
- Redis 8.6.2/Search 8.6.0, RESP3 on every path, four server CPU cores, 2 GiB,
  dedicated container, persistence disabled, localhost TCP without TLS.
  Shared macOS arm64 host, 18 logical CPUs/36 GiB. Other user workloads were not
  stopped; paired/interleaved trials reduce order effects but do not create a
  dedicated performance lab. No 100M-row, WAN/TLS or production ranking claim.
- Python 3.13.16, redis-py 8.1.0, hiredis 3.5.0, PyArrow 26.0.0,
  adbc-driver-manager 1.12.0. Reusing the #422 binary and environment isolates
  this test from later driver/dependency upgrades.

## Reproduce

Use a dedicated Redis server. `--load` replaces only this runner's
`transferbench.hits` fixture; do not point it at an existing user dataset.

```sh
python benchmarks/incremental-read/run.py \
  --driver /absolute/pinned-422.dylib \
  --uri 'redis://127.0.0.1:18445/0?protocol=3' \
  --parquet /absolute/hits-prefix-374.parquet \
  --clickbench /absolute/ClickBench \
  --work /absolute/new-output-directory --load --repeats 6
```

The driver binary SHA-256 is
`24a5ad1697e648bd8af931383f05e85d1afab3ce4460d498818dc4dfd2ea371f`.
The source Parquet SHA-256 is
`d053397f7aa5b2e1ee9160779a9e9307b162c6994aebba829c7ea1411076c4d0`.
Build/source equivalence is recorded in the
[original #422 evidence](../../hash-slice-arrow/2026-10-21/README.md).
The runner reuses the existing independent fixture and native discovery helpers
from `benchmarks/wide-transfer/compare.py`; both runner hashes are pinned in
[raw results](results.json). [Manifest](manifest.json) pins environment/scope.
[Smoke results](smoke-results.json) are separate from timed confirmation.
