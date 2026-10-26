# Local full-result Arrow transfer comparison

> Historical evidence, published October 22, 2026. See the
> [archive notes](../README.md) for provenance, RESP2 settings and newer results.
> The timings below describe the original runs, not the current driver.

Measured October 21, 2026. This is a 100,000-row local full-result download comparison, not an official ClickBench score or a 100-million-row transfer benchmark.

## Result

Each execution returns all 100,000 rows and all 105 columns from the same official ClickBench prefix: **79,228,019 logical Arrow bytes (75.5577 MiB)**. Every run materializes its complete result as Arrow IPC files. All **32 executions** passed exact comparison of every value and row multiplicity against the typed source, including eight warm-ups and 24 measured runs.

| Engine and client path | One reader, median s | Four readers / eight partitions, median s | Four-reader rows/s | Four-reader Arrow MiB/s |
|---|---:|---:|---:|---:|
| Redis 8.6.2 / Redis ADBC | 5.338476 | 2.829811 | 35,338 | 26.701 |
| PostgreSQL 16.10 / PostgreSQL ADBC | 0.624078 | 0.543334 | 184,049 | 139.063 |
| ClickHouse 25.8.33.6 / HTTP ArrowStream | 0.291018 | 0.297743 | 335,861 | 253.769 |
| DuckDB 1.5.6 / embedded Arrow reference | 0.218514 | 0.230206 | 434,393 | 328.218 |

Redis was slower than PostgreSQL and ClickHouse on this local wide-row read. Four-reader Redis was about 1.89x faster than its own single-reader profile. Those are results for these data, versions and paths, not general product rankings. The earlier 3.2067-second Redis result used a different recorded binary/harness run; the matched comparison reran Redis and records 2.829811 seconds.

## Timing contract

- One untimed-but-validated warm-up and three measured executions for each engine/profile. Report medians and retain min/max and every trial. Engine/profile order rotates between measured rounds. No cold-cache claim.
- Start the timer before process-pool creation and Redis partition planning. End it after all workers have fetched all batches, normalized to the same physical Arrow schema, written IPC files, closed connections and exited. This includes process imports, native driver startup, connections, query execution, complete fetch, Arrow normalization, IPC writes and worker teardown.
- Loading, source decoding, index-readiness checks and exact output validation occur outside the timer. Output validation reads every IPC result, sorts all 105 fields and compares complete Arrow tables, preserving multiplicity.
- One-reader profile: one fresh spawned worker scans all selected columns without sorting. Four-reader profile: four fresh spawned workers process eight disjoint, exhaustive ranges. Redis uses its partition API. Other engines use equivalent synthetic ordinal ranges. Only the original 105 columns are returned.
- Redis uses its managed row index. PostgreSQL has a B-tree index on the added ordinal. ClickHouse uses MergeTree ordered by the ordinal. DuckDB stores an ordinal column with the loaded table. These access structures and native client protocols differ intentionally; this is an end-to-end access-path comparison, not an isolated transport microbenchmark.
- The payload rate uses the same uncompressed logical Arrow bytes for each engine. It is not NIC throughput. IPC framing and protocol compression/overhead vary. IPC bytes are retained per worker.

## Environment and limits

- macOS 26.7.1 arm64, 18 logical host CPUs, 36 GiB host RAM. Clients run on the host. Server engines run in separate native-arm64 Docker containers, each limited to four CPU cores and 2 GiB RAM. No TLS or WAN delay.
- Docker image identities and limits are in `containers.txt`. Client versions, source hash, driver hash and server versions are in `manifest.json`. The exact Redis binary is pinned by SHA-256. Installed package metadata says 0.1.0; its source revision was not independently attested, so this report does not substitute the current checkout SHA for the binary identity.
- DuckDB is a separate embedded reference, with four threads and a 2GB memory setting per connection. It does not cross Docker/TCP and has no equivalent process CPU/memory quota. Exclude it from a like-for-like server transfer ranking.
- Three repeats are a small local sample. Host and Docker resources are shared with other work. Sampled/lifetime worker RSS is diagnostic and does not measure isolated server peak memory.
- The 100,000-row sequential source prefix is not a representative random sample of the complete dataset. No production performance, 100-million-row scaling or optimal tuning claim follows.
- No public/shared database or cloud resources were modified. Only dedicated task-owned containers and temporary data files were used.

## Reproduce

Use fresh, dedicated Redis 8.6.2, PostgreSQL 16.10 and ClickHouse 25.8 containers. See the connection constants in `../benchmark.py`; all bind to localhost and use throwaway local credentials. Apply `--cpus=4 --memory=2g` to each server. Do not run against existing user data because the loader creates a table named hits.

Install pyarrow==26.0.0, duckdb==1.5.6, redis==8.1.0, psutil==7.2.2, adbc-driver-manager==1.12.0 and adbc-driver-postgresql==1.12.0 in an isolated Python 3.13 environment. Provide the exact Redis driver binary identified in the manifest, or treat another binary as a new result.

```sh
python benchmarks/transfer-comparison/benchmark.py \
  --driver /absolute/path/to/libadbc_driver_redis.so \
  --parquet /absolute/path/to/hits-prefix-100k.parquet \
  --upstream /absolute/path/to/ClickBench \
  --work /absolute/path/to/fresh-output
```

Source prefix SHA-256: `d053397f7aa5b2e1ee9160779a9e9307b162c6994aebba829c7ea1411076c4d0`. Official ClickBench PostgreSQL DDL checkout: `ccda3033f3c7303d9d7f9634d6dae118ef085f96`. The benchmark converts epoch-day dates and epoch-second timestamps using this repository's existing fixture conversion helper. Source data is not included in the evidence bundle.

## Evidence

- `results.json`: all raw trials, elapsed times, first-batch times, IPC sizes, validation status and summaries.
- `manifest.json`: source, driver, script, versions and environment identities.
- `containers.txt`: exact container image identities and enforced server limits.
- `trials.log`: completion log.
- `benchmark-original.py`: exact original cross-product harness snapshot, matching the recorded script hash.
- `../benchmark.py`: extended reproducible harness including the native-client tests below.

## Paired Redis native-client extension

A separate paired rerun on the same 100,000-row/105-column fixture compares Redis ADBC with official redis-py 8.1.0 and hiredis 3.5.0. The same native-arm64 Redis 8.6.2 image is limited to four CPU cores and 2 GiB RAM. All **24 executions** passed full-value validation (six warm-ups and 18 measured trials).

| Path | Output in timed path | One reader median s | Four readers / eight ranges median s | Four-reader rows/s |
|---|---|---:|---:|---:|
| Redis ADBC | Typed Arrow IPC | 5.383905 | 2.971782 | 33,650 |
| redis-py pipelined HMGET | Typed Arrow IPC | 3.299396 | 1.610503 | 62,092 |
| redis-py pipelined HMGET | Native Python bytes, pickle IPC | 2.785682 | 1.489078 | 67,156 |

- Native reads use a connection pool per worker, non-transactional pipelines of up to 1,000 HMGET commands, and the accelerated hiredis parser. They use known dense row keys and a fixed schema. They do not perform SQL planning, arbitrary key discovery, filters, schema discovery or generic ADBC behavior.
- The Arrow path converts stored byte values to exact typed columns and writes Arrow IPC during timing. The no-Arrow path returns native Python byte values and writes pickle protocol-5 files during timing. This is a different output contract. It does not convert rows to Arrow inside the timed path. The common harness still imports its shared dependencies.
- Worker startup, connections, all Redis reads, output serialization, file writes and worker teardown are included for both paths. After the timer stops, the no-Arrow outputs are decoded solely for exact validation. No data-transfer-only or in-process raw-GET latency claim is made.
- The logical typed dataset is 79,228,019 Arrow bytes, but native byte strings and pickle have different serialized sizes. Do not label the no-Arrow result as Arrow MiB/s or NIC bandwidth. Actual output file sizes are retained per worker in native-output/results.json.
- The cross-product table above and this paired Redis extension are separate runs; keep their matched control measurements separate. The intermediate native-arrow/ directory retains the first 16 validated paired executions from before the additional no-Arrow request, with its exact script snapshot. The final native-output/ comparison is the result used for the three-path slide.
- Official references: [redis-py](https://github.com/redis/redis-py) and [Redis pipeline guidance](https://redis.io/docs/latest/develop/clients/redis-py/transpipe/).

Reproduce the extension after loading fresh dedicated Redis data (the loader skips duplicate native paths):

```sh
python benchmarks/transfer-comparison/benchmark.py \
  --driver /absolute/path/to/libadbc_driver_redis.so \
  --parquet /absolute/path/to/hits-prefix-100k.parquet \
  --upstream /absolute/path/to/ClickBench \
  --work /absolute/path/to/fresh-output \
  --engines redis redis_py redis_py_raw
```

Additional dependency: hiredis==3.5.0. Use native-output/manifest.json and native-output/benchmark.py to identify this extension exactly.
