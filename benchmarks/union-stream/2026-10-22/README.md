# Incremental UNION ALL: issue #459

Measured October 22, 2026, on Redis Software Enterprise 8.6.2, with active
QPF 4x and effective six Search workers, mnp routing and 32 connections verified.
This is a local, two-column synthetic SQL experiment, not ClickBench.

The new reader executes eligible UNION ALL trees sequentially, retaining one
active child scan and output batches instead of complete branch results. It
keeps duplicate rows and the pairwise type-coercion tree. Its initial scope is
managed indexed table scans without ORDER BY; blocking/incompatible plans keep
the established materialized executor.

## Peak process RSS

Each cell is the median of three fresh processes, in MiB (1,048,576 bytes).
Every process fully consumed and independently checked every key and label,
output count, schema, and the streamed batch row ceiling. **36/36 trials passed**,
covering 3,840,000 output rows in total.

| Input rows per branch | Output rows | Distinct output keys | Materialized RSS | Streamed RSS |
|---:|---:|---:|---:|---:|
| 10,000 | 20,000 | 1 | 115.0 | 97.7 |
| 10,000 | 20,000 | 10,000 | 113.9 | 98.1 |
| 50,000 | 100,000 | 1 | 246.4 | 106.4 |
| 50,000 | 100,000 | 50,000 | 241.1 | 107.1 |
| 100,000 | 200,000 | 1 | 386.0 | 107.7 |
| 100,000 | 200,000 | 100,000 | 397.7 | 106.9 |

There is no deduplication/group state in UNION ALL: output-key cardinality varies
without requiring a distinct set. Higher input cardinality still increases the
materialized result's retained rows. The streamed reader's process peak changes
with cursor pages, output buffers, runtime allocation/GC and process baseline;
these observations do not establish constant RSS or bounds for other operators.

## Method and limits

- Same candidate native library, alternating `adbc.redis.stream_results=false`
  and `true`: false/true, true/false, false/true across three trial pairs. No
  separate historical binary or changed SQL is used for the comparator.
- Input fixtures contain dense BIGINT IDs 0 through N-1 and a 128-byte ASCII
  text value. Query: `SELECT id % C AS k,label FROM fixture UNION ALL SELECT
  id % C AS k,label FROM fixture`, with C=1 or N. The two identical branches
  deliberately preserve all duplicates. This does not test concurrent writes,
  many-child trees, wide schemas, variable-width distributions or WAN/TLS.
- Streaming output uses 1,024-row batches and the default disabled byte budget.
  The materialized path retains its normal 65,536-row Arrow batches (1/2/4
  batches at these output sizes); the streaming path emits 20/98/196. Thus the
  comparison includes both native materialization and their actual consumer
  batch footprints. It does not isolate native heap allocation alone.
- `resource.getrusage(RUSAGE_SELF).ru_maxrss` includes Python, native Go/Arrow,
  and batch-by-batch validation allocations. It excludes the parent fixture
  loader and Redis server. The approximately 67 MiB process baseline before
  execution is recorded, not subtracted. No complete consumer result is kept.
- Every value is checked against its independent expected position modulo N
  then C, and `"x" * 128`. Result counts equal 2*N; streamed counts initially
  report -1. This validation does not rely on the materialized comparator.
- Raw timing is retained for reproducibility and includes validation; there is
  no isolated driver latency or throughput claim. Server data is reused across
  pairs; there is no cold-server/warmup control or dedicated host isolation.
- Local Docker Redis Software: one primary shard, 1 GiB database, noeviction,
  persistence disabled, localhost RESP3 TCP without TLS. macOS arm64 client,
  Python 3.12, PyArrow 26.0.0, ADBC manager 1.12.0, Go 1.26.10. Other user
  processes may share the host. This task's container and network are owned
  under Compose project `adbc-union459`; no paid deployment was created.

[Raw results](results.json) and [manifest/checksums](manifest.json) pin the scope,
measurements, environment and tested library. The measured library was rebuilt from the final production source; its
source and binary checksums are recorded in the manifest.

## Reproduce

Build with `make -C go build`, install the Python package and PyArrow, and use a
dedicated disposable Redis Software database with verified effective QPF 4x:

```sh
PYTHONPATH=python REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
python benchmarks/union-stream/run.py \
  --uri redis://127.0.0.1:36379/0 \
  --output /absolute/new-results.json --sizes 10000 50000 100000 --repeats 3
```

The runner creates uniquely named `it_union_memory_*` fixtures and drops them in
finally blocks. Set the shared-library filename appropriately for your platform.
No cross-platform measurement is claimed by this report.
