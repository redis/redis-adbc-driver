# Bulk loading: Redis ADBC versus redis-py

Measured October 22, 2026. **100,000 rows × 105 columns**, independently typed from the official ClickBench prefix. This measures loading an Arrow dataset into indexed Redis HASHes.

With one writer, ADBC took **7.575 s** versus **9.712 s** for redis-py/hiredis: **22.0% less elapsed time**. Every measured one-writer ADBC run was faster than every measured native run. With four writers, the medians were **6.402 s** and **6.546 s**; their ranges overlap, so this does not establish a clear four-writer winner.

## Results

One warmup followed by six measured executions per case; case order reverses on alternating rounds. Medians and observed ranges, not confidence intervals.

| Client | Writers | All rows written and indexed, seconds | Last write acknowledged, seconds | Rows/s |
|---|---:|---:|---:|---:|
| Redis ADBC | 1 | **7.575** (7.403–7.675) | 7.545 | 13,201 |
| redis-py + hiredis | 1 | **9.712** (9.645–9.836) | 9.690 | 10,296 |
| Redis ADBC | 4 | **6.402** (6.169–6.853) | 6.349 | 15,620 |
| redis-py + hiredis | 4 | **6.546** (6.205–6.690) | 6.511 | 15,276 |

All **28 canonical executions** (four warmups plus 24 measured) and **four final smoke executions** passed independent checks of every stored value and row multiplicity, dense row IDs, row-ID counter, Search document count and indexing-failure status. The same 106 Search attributes (105 source columns plus `__rowid`) were present throughout.

## What is timed

- Start before identical empty-table/index creation through ADBC for both clients. Both receive the same typed Arrow IPC file.
- Include fresh Python worker startup/imports, Arrow input reads, connections, conversion to the client representation, row-ID allocation, every HSET, worker teardown and index readiness. The last-write column uses the same start boundary and stops at the last worker acknowledgement.
- Source parquet conversion, clearing the dedicated database between trials, and full independent validation happen outside the timer. There is no preconverted Python-mapping advantage: redis-py converts Arrow columns inside its timed work.
- One or four fresh writer processes; each writes an equal share of rows. Arrow batches contain at most 10,000 rows. Both paths send at most 1,000 HSET commands per pipeline, matching the driver's internal write limit.
- Native redis-py uses nontransactional pipelines, hiredis and INCRBY row-ID allocation. It writes the same field encodings and indexable HASHes but omits generic SQL constraints, metadata fencing and concurrent-DDL handling. It is a fixed-schema, non-null fixture writer, not an alternate general ADBC implementation.
- Both paths use RESP3. Persistence is disabled, and no durability/fsync guarantee is measured.

The common setup median was about 0.027 s in every case. The final index check after worker shutdown took about 0.001 s: indexing largely overlapped the writes. End-to-end time includes the entire load and readiness boundary.

## Resource tradeoffs

| Client | Writers | Worker CPU, median seconds (sum) | Server CPU, median seconds | Largest worker lifetime peak RSS, MiB |
|---|---:|---:|---:|---:|
| Redis ADBC | 1 | 2.495 | 5.893 | 722.6 |
| redis-py + hiredis | 1 | 4.045 | 5.918 | 215.1 |
| Redis ADBC | 4 | 4.505 | 5.968 | 645.5 |
| redis-py + hiredis | 4 | 4.491 | 6.067 | 212.2 |

ADBC uses less worker CPU in the one-writer case but more peak process memory. RSS includes the worker runtime and Arrow input, is not a simultaneous sum across workers, and is not isolated driver allocation. Similar server CPU and overlapping four-writer times suggest that adding client writers narrows the client-side difference; a server profile would be needed for stronger attribution.

## Environment and identity

- Driver source: `d72b730bf24074942609e9f0f839c1f46e10d365`; built with the repository Makefile and its version linker flag. No production code changes for this experiment.
- Shared library SHA-256: `3e3fce4a85a22f31933b02fa360acdc8be66490295721aa099e9934b51edb934`.
- Measured runner SHA-256: `c187634b4c954a517dd9bd331eb58e36ff2284c5bda31317a3b0d38ffc4205a1`.
- Source parquet SHA-256: `d053397f7aa5b2e1ee9160779a9e9307b162c6994aebba829c7ea1411076c4d0`. Typed Arrow payload: 79,578,019 bytes.
- Official ClickBench DDL commit: `ccda3033f3c7303d9d7f9634d6dae118ef085f96`. Fixture helper hash is recorded in the raw results.
- Redis 8.6.2 / Search 8.6.0; dedicated native-arm64 Docker container limited to four CPUs and 2 GiB. Localhost TCP; no TLS, RDB saves or AOF. Image and port/limits: [container.txt](container.txt).
- macOS 26.7.1 arm64 client, 18 logical CPUs, 36 GiB RAM. Go 1.27.2; Python 3.13.16; PyArrow 26.0.0; redis-py 8.1.0; hiredis 3.5.0; ADBC driver manager 1.12.0.
- Shared workstation/Docker host; six repeats, one server and a sequential source prefix. Results do not establish WAN/TLS performance, production throughput, durability, 100-million-row loading time, prebuilt Python-object performance or a generic client ranking.

## Evidence and checks

- [Canonical raw results](results.json) and [trial log](trials.log): all timings, worker resources, Redis command counters and exact-validation flags.
- [Final smoke results](smoke-results.json) and [log](smoke.log): all four cases pass.
- Earlier harness-only failures are retained: [JSON byte-key serialization](smoke-failed-serialization.log) and [protocol-value type comparison](smoke-failed-protocol-check.log), with the latter [partial result](smoke-failed-protocol-check.json). They are excluded from the canonical matrix; the failure was in diagnostic bookkeeping, not a loaded-data mismatch.
- Four focused helper tests cover exact BIGINT/NUL/UTF-8/date/microsecond conversion, rejection of null/unsupported fixture types and atomic RESP3 metadata serialization.
- Publication checks recompute all summaries, verify artifact/source hashes and check local report links. No current production Go regression suite was rerun because no production code changed.

## Reproduce

Requires a fresh dedicated Redis server: this harness clears its owned database before every trial and refuses a nonempty server at startup. Use the exact fixture, dependency versions and source identified above.

```sh
make -C go build
docker run -d --name redis-bulk-load-repro --cpus 4 --memory 2g \
  -p 127.0.0.1:18446:6379 redis:8.6.2 \
  redis-server --save '' --appendonly no
python benchmarks/bulk-load/run.py \
  --driver "$PWD/go/build/libadbc_driver_redis.dylib" \
  --uri 'redis://127.0.0.1:18446/0?protocol=3' \
  --parquet /absolute/path/to/hits-prefix-100k.parquet \
  --clickbench /absolute/path/to/ClickBench \
  --work /absolute/path/to/fresh-output --repeats 6 --dedicated-empty-server
python -m unittest discover -s benchmarks/bulk-load -p 'test_*.py' -v
docker rm -f redis-bulk-load-repro
```

Use the shared-library suffix appropriate to the host. The input Arrow file is generated from the parquet source and is not checked in. Raw results pin the exact measured runner; a modified rerun produces new evidence.
