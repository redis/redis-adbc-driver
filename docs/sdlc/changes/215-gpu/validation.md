# Validation: #215 GPU feasibility

Date: 2026-11-03 (America/Los_Angeles). Driver baseline:
`95d6583d2b07bdfdadc6ddf3b6368bf631139c24`; experiment scripts are this change.
Status: CPU/CUDA checks and Redis Software comparison passed; no driver GPU
backend recommended from these measurements.

## Actual checks

- Local macOS/arm64, PyArrow 26.0.0: six CPU correctness tests passed; one
  explicit CUDA-only skip. A 100,000-row, three-trial CPU smoke completed with
  every output independently validated. The sandbox denied CPU cache sysctl
  reads; Arrow continued and returned correct results.
- EC2 `g6.2xlarge`, Ubuntu 24.04, 8 vCPUs, 32 GiB RAM, one NVIDIA L4:
  `GPU_TEST=1 python -m unittest discover -p 'test_*.py' -v` passed all seven
  tests, including real CUDA NULL groups, empty/all-filtered input and integers
  above 2^53. NVIDIA driver 595.112.07; runtime/package versions are in the raw
  reports.
- Real CUDA synthetic trials: one warmup and three alternating CPU/GPU trials
  at 100k, 1m and 5m rows. All results matched the exact oracle. See
  [timings and raw reports](../../../../benchmarks/gpu-query/README.md#initial-ec2-synthetic-measurements).
- Python syntax parsed for all three scripts; `git diff --check` passed.
- Linux `make -C go build` succeeded on the pinned revision. SSM shells require
  explicit `GOPATH` and `GOCACHE` because their home environment can be unset.
- `make -C go redis-up` succeeded with Redis Software, Redis 8.6.2 and Search.
- Verified active 4× QPF, six Search workers, `mnp` routing and 32 connections
  before integration checks. Search version 8.6.11. Loaded one million rows
  into a new disposable table; full fetch matched generated values and
  multiplicities. One warmup and three timed Redis trials passed group/value
  and physical int64 type checks for direct SQL, CPU Arrow and CUDA results.
- Direct SQL execute-plus-fetch median: 7.4005 s. Input scan median: 9.7001 s.
  CUDA upload/compute/download median: 0.01240 s. Modeled scan plus CUDA median:
  9.7127 s. Thus GPU computation did not improve this query's cost. The modeled
  sum excludes oracle/domain preflight and is not a measured native GPU query.
  [Raw reports and environment](../../../../benchmarks/gpu-query/README.md#redis-software-comparison-on-the-same-ec2-host)
  preserve each trial and its scope.

Benchmark script SHA-256:
`980fcac44332d5edaa3aea72bd4a55e018380c1fc4692452617fd9581bac49b8`.

## Review

Self-review using `REVIEW.md`: correctness checks cover exact bounded integer
aggregation, NULLs, empty input, duplicate groups, wrong results/types, overflow
rejection and identifier quoting. CUDA is opt-in and fails closed. The shared
scan estimate is labeled separately from measured direct SQL time. No driver
runtime, C ABI, default dependency or existing execution path was modified.

The fixture loader creates only a new table in a local disposable database and
checks fetched value multiplicities. SQL data and connection credentials are
excluded from benchmark reports; exception details are suppressed by the runner.
Native GPU cancellation, spilling and C Device lifecycle remain unimplemented.
This self-review is not maintainer approval.

## Limits and operational cleanup

Synthetic operator speedups do not prove end-to-end Redis speedups. The initial
experiment covers one bounded integer filter/groupby workload and one GPU model.
No general SQL, float/decimal exactness or native device output claim is made.
Temporary cloud resources and operational identifiers are tracked privately;
the EC2 instance is confirmed terminated. The temporary SSH key registration,
security group, SSM IAM role/profile and local private key were removed. The
auto-delete root disk was observed in AWS's `deleting` state after termination.
