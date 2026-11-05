# Host-staged GPU query experiment

This opt-in harness investigates [issue #215](https://github.com/redis/redis-adbc-driver/issues/215).
It does not enable GPU execution in the driver. Read the
[design and limitations](../../docs/gpu-query-feasibility.md).

Use a Python environment with PyArrow. CPU smoke and correctness checks:

```sh
python -m unittest discover -s benchmarks/gpu-query -p 'test_*.py' -v
python benchmarks/gpu-query/run.py --rows 100000 --groups 1000
```

On a Linux NVIDIA CUDA machine, install a compatible cuDF/CuPy environment
using the [RAPIDS installation guide](https://docs.rapids.ai/install/).
For example, a single-GPU EC2 G6 instance provides an NVIDIA L4; AWS documents
the [GPU families](https://docs.aws.amazon.com/dlami/latest/devguide/gpu.html)
and [NVIDIA driver setup](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/install-nvidia-driver.html).
Run `nvidia-smi` first, then:

```sh
python benchmarks/gpu-query/run.py --backend cuda \
  --rows 1000000 --groups 1000 --warmups 1 --trials 3 > synthetic.json
```

CUDA mode runs both CPU Arrow and GPU cuDF on the same host Arrow input,
alternating backend order, checking every result against exact Python integers.
No CUDA library is imported in CPU mode. A requested CUDA failure exits nonzero
and emits no successful report. Results include GPU model/memory, CUDA runtime
and driver versions, package versions, row/group counts and Arrow input bytes.
Warmup results are excluded. The CUDA host-to-host interval includes allocation,
upload, filter/groupby/aggregation, synchronization and download; it excludes
environment initialization, input generation, domain preflight and validation.
The CPU interval includes filtering and Arrow grouping. This is a small bounded
integer workload, not a general SQL benchmark.

## Read-only Redis comparison

Use a stable existing table with nullable BIGINT `bucket` and `value` columns.
The runner does not create, ingest or drop tables. Stop writers while comparing:
the driver has no snapshot transaction across the scan and direct SQL queries.
The exact oracle rejects values/types outside the experiment's domain. Memory
must accommodate the whole host table, Python oracle and GPU intermediates;
this prototype is not a streaming or spilling engine.

Build the driver on the Linux host with `make -C go build`. Use Redis Software
with Search and verified active 4x QPF; `make -C go redis-up` provisions the
repository test environment. Supply `REDIS_URI` privately, then:

```sh
python benchmarks/gpu-query/run.py --backend cuda \
  --driver go/build/libadbc_driver_redis.so --schema public --table gpu_fixture \
  --warmups 1 --trials 3 > redis-comparison.json
```

To create a new fixture in that disposable local database first:

```sh
python benchmarks/gpu-query/load_fixture.py \
  --driver go/build/libadbc_driver_redis.so --rows 1000000
```

The loader uses create-only ingestion, refuses non-local endpoints and validates
all fetched values and multiplicities against the generated data. It leaves the
table for inspection and refuses to overwrite an existing table.

Redis mode requires `adbc-driver-manager` in addition to PyArrow/cuDF/CuPy.
The two source columns are fully fetched. The equivalent aggregate SQL is
separately executed and fully fetched, alternating before/after the scan.
All groups, values and int64 physical output types are validated, ignoring
unordered group order. A mismatch aborts the report.

`scan_fetch_seconds` includes scan execute and complete Arrow fetch.
`driver_sql_seconds` includes aggregate execute and complete Arrow fetch.
`estimated_scan_plus_backend_seconds` adds the shared scan time to the measured
backend host-to-host time. It is a cost estimate; it excludes preflight and is
not an independently timed query or a reported speedup. Repeated GPU work on
already resident inputs may behave differently. Run several row counts and
group cardinalities before drawing conclusions. Keep credentials, raw errors,
operational IDs and connection details out of committed reports.

## Initial EC2 synthetic measurements

Measured 2026-11-03 on one `g6.2xlarge` (8 vCPUs, 32 GiB RAM, NVIDIA L4).
One warmup and three alternating trials per size; 1,000 bucket values plus a
NULL group. Seven tests passed on CUDA, including exact integers above 2^53,
NULL groups and empty/all-filtered input. Median milliseconds:

| Input rows | CPU Arrow | GPU compute | GPU including upload/download | CPU time / GPU total |
|---:|---:|---:|---:|---:|
| 100,000 | 2.77 | 4.10 | 5.74 | 0.48× |
| 1,000,000 | 19.14 | 5.72 | 8.90 | 2.15× |
| 5,000,000 | 61.63 | 11.28 | 19.66 | 3.13× |

Raw reports: [100k](synthetic-100000.json), [1m](synthetic-1000000.json),
[5m](synthetic-5000000.json). CPU wins at the smallest size. These are
host-resident operator measurements; no Redis fetch is included. This narrow
integer workload does not establish general SQL or end-to-end Redis speedup.

## Redis Software comparison on the same EC2 host

One million generated rows were ingested into a new disposable table and fully
read back; values, NULLs and multiplicities matched the generator. Redis 8.6.2,
Search 8.6.11, one shard, active 4× QPF, six Search workers, `mnp` routing and
32 connections were verified. Driver options used their defaults, including
exact aggregate pushdown and automatic workers. Client and server shared the
EC2 host, communicating over loopback. One warmup and three trials passed full
result/type validation. Median times:

| Operation | Seconds |
|---|---:|
| Direct driver SQL, execute plus full fetch | 7.4005 |
| Input scan, execute plus full fetch | 9.7001 |
| CPU Arrow filter/group/reduce on fetched input | 0.01238 |
| CUDA upload, filter/group/reduce, result download | 0.01240 |
| Estimated shared scan plus CUDA backend | 9.7127 |

The GPU consumer path offers no improvement for this query: fetching the input
dominates, and its modeled cost exceeds direct SQL by about 31%. The final row
is a sum of measured components, not an independently timed GPU SQL query.
The fetched Arrow input has different chunking/allocation history from the
synthetic input; the standalone synthetic operator ratio does not transfer to
this run. A driver-integrated backend would need to avoid those conversion and
fetch costs or amortize them across substantial additional work.

Evidence: [raw trials](redis-1000000.json),
[environment](redis-environment.json), [fixture validation](fixture-validation.json).
This is one workload on one shared client/server host; general GPU query
execution and an automatic planner backend remain unproven. Native device
interchange remains deferred as described in the feasibility assessment.
