# v0.0.18 benchmark refresh — October 29, 2026

[Results and limitations](report.md) · [Change intent](../../../docs/sdlc/changes/v0018-benchmark-refresh/intent.md)

This bundle records the seven local suites used to update presentation slides
8–15. The 100-million-row tests on slides 16–18 were explicitly deferred. Their
older Redis Community results must remain labeled as historical.

## Audit without a database

From the repository root, using Python 3.10 or newer:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 benchmarks/v0018-refresh/2026-10-29/audit.py
```

The audit recomputes the IPC comparisons and predeclared worker-policy gates,
checks repetition coverage, validates the recorded correctness flags, recomputes
visible medians/ranges and confirms the published summary. It does not rerun SQL
or independently recover discarded result rows. Original runs completed full
result consumption and the validators described in the report.

Adaptive workers passed **10/12 performance gates**. The two misses are at 8× QPF
with one and two concurrent statements. All query results passed correctness
checks; a performance-gate miss is not a wrong-result failure.

## Recorded workload matrix

| Slides | Harness from repository root | Parameters | Completed |
|---|---|---|---|
| 8 | `benchmarks/bulk-load/run.py` | 100K × 105; 1/4 writers; 6 repetitions after warmup | 28 loads |
| 9–10 | `benchmarks/incremental-read/run.py` | 100K × 105; 512/4096 batches; 1/4 readers; 6 repetitions | 98 executions |
| 11 | `benchmarks/union-stream/run.py` | 10K/50K/100K input rows; 1/N cardinality; 3 repetitions | 36 executions |
| 12 | `benchmarks/native-ipc/run.py` | 50K synthetic rows; 5 repetitions after warmup | 144 operations |
| 13 | `benchmarks/qpf/adbc.py` | 100K sample rows; 3 rounds; 5-second windows | 90 trials |
| 14 | `benchmarks/worker-autotune/controlled/qpf_matrix.py` | 32768 rows; 60 learning + 18 measured rounds; 3 repetitions | 48 trials / 144 concurrency records |
| 15 | `benchmarks/wide-transfer/cross_database.py` | 100K × 105; 1/4 reused/fresh readers; 5 repetitions | 72 executions |

## Repeat the measurements

Use a separate checkout of public v0.0.18 commit
`2f053d3affad90a1d2798c000f0d1825183e6e54`, plus the bulk-load runner and tests
introduced by this change. Do not label a current-main binary v0.0.18. Build
with `make -C go build VERSION=v0.0.18` in a clean build directory and confirm
GetInfo reports v0.0.18. The report records the actual toolchain, client versions,
source hashes and fixture hashes. `harness-sha256.json` identifies the measured
harness revisions.

Use Python 3.13.16, PyArrow 26.0.0, ADBC manager/PostgreSQL driver 1.12.0,
redis-py 8.1.0 and hiredis 3.5.0 to match this run. Set `PYTHONPATH` to that
checkout's `python` directory and `REDIS_ADBC_LIBRARY` to its compiled library.
The QPF sample helper loads `go/build/libadbc_driver_redis` directly, so run it
from the same checkout whose binary was just verified.

Create an empty, dedicated Redis Software fixture with `make -C go redis-up`.
Its setup verifies active **4× QPF**, six Search workers, `mnp` routing and 32
connections; verify these effective settings again before timing. Use RESP3.
Do not use Redis Community or a shared application database. The bulk runner
requires `--dedicated-empty-server` and flushes its owned fixture between trials.
Use new output directories and reset only the task-owned fixture between suites.

The ordinary suites use one shard, a four-CPU/8-GiB server container, and a 2-GiB
database limit; UNION memory uses a 1-GiB database limit. The policy matrix uses
a 16-CPU server ceiling and a two-CPU/2-GiB Linux client. These limits, shared-host
conditions and differing historical environments matter to interpretation.
QPF sweeps intentionally change factors, verify effective settings, and restore
4×. They use synthetic sales/aggregate fixtures, not the full-scale ClickBench
query suite. Keep deployment names, endpoint values, API credentials and license
files in private configuration.

The wide fixture is the first 100000 rows of the official
[ClickBench Parquet dataset](https://datasets.clickhouse.com/hits_compatible/hits.parquet).
Use ClickBench DDL revision `ccda3033f3c7303d9d7f9634d6dae118ef085f96`.
Fixture extraction and native normalization must match the hashes in the report
before timing. Loading is part of the requested bulk test; the other local
suites prepare the small fixture outside timing. No full-scale reload occurred.

Invoke each harness with `--help` for required private endpoint and output
arguments. The measured controls were:

```text
bulk-load/run.py: --repeats 6 --dedicated-empty-server
incremental-read/run.py: --load --repeats 6
union-stream/run.py: --sizes 10000 50000 100000 --repeats 3
native-ipc/run.py: --rows 50000 --repeats 5
wide-transfer/cross_database.py: --load --repeats 5
qpf/adbc.py: --rounds 3 --seconds 5
worker-autotune/controlled/qpf_matrix.py:
  --cpu 2 --rows 32768 --warmup 60 --rounds 18 --repetitions 3 --modes waves
```

Load `go/examples/load_sample.py --rows 100000` before the QPF throughput suite.
Compile the policy test binary from the same v0.0.18 source for Linux arm64 using
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c`, preserving the version linker
flag from the Makefile. The policy harness requires explicit private Docker
network/server/endpoint arguments. Its status 2 means completed measurements
missed performance gates; status 1 or missing trials indicates infrastructure
failure. Inspect all results before publishing.

For the product comparison, provision PostgreSQL 16.10 and ClickHouse 25.8.33.6
on the same host with four CPUs/8 GiB each. PostgreSQL fsync, synchronous commit
and full-page writes were disabled; ClickHouse used uncompressed ArrowStream.
These are resident-data query/transfer tests, not durability or equal-cost tests.
Clean up only owned fixtures after retaining private raw evidence.

## Publication boundary

Numeric trials, correctness status, necessary test settings, source hashes and
summary arithmetic are included. Operational manifests, infrastructure IDs,
credentials, licenses, complete logs, generated binaries, datasets and Google
Slides edit payloads are excluded. QPF configuration snapshots retain only the
factor, Search worker count, routing and connections needed to verify the sweep.

This is evidence for the measured v0.0.18 build, not validation of later driver
changes. No production driver behavior is changed by this PR.
