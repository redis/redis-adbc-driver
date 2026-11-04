# Validation: #215 GPU feasibility

Date: 2026-11-03 (America/Los_Angeles). Driver baseline:
`95d6583d2b07bdfdadc6ddf3b6368bf631139c24`; experiment scripts are this change.
The original consumer-prototype investigation passed CPU/CUDA checks and the
Redis Software comparison. The subsequently requested native CUDA/C Device
implementation is under validation; its separate evidence is below.

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
That review covered the original prototype. Native implementation review and
validation are tracked separately below; general GPU spilling remains out of scope.
This self-review is not maintainer approval.

## Limits and operational cleanup

Synthetic operator speedups do not prove end-to-end Redis speedups. The initial
experiment covers one bounded integer filter/groupby workload and one GPU model.
No general SQL, float/decimal exactness or native device output claim is made.
Temporary cloud resources and operational identifiers are tracked privately;
the EC2 instance is confirmed terminated. The temporary SSH key registration,
security group, SSM IAM role/profile and local private key were removed. The
auto-delete root disk was observed in AWS's `deleting` state after termination.

## Native implementation on current main

Base: `6d7ac409d8c9f11163b4f4cec63e2d4da00e69f2`, after transplanting this task's
changes without the older checkout's unrelated history. Current main dependency
and TLS/RESP3/partition/release improvements were preserved.

- Current-main unit-only suite passed with Go 1.27.3: 887 test/subtest passes,
  zero failures and 597 test/subtest skips. The new CUDA shape/option, NULL/order,
  cancellation and fail-closed tests passed.
- Full ordinary Software suite passed on the isolated verified 4×-QPF fixture:
  1,716 test/subtest passes, 40 explicit capability/profile skips, zero failures.
  These are CPU execution checks; they do not substitute for CUDA validation.
- CPU-only CLI built with `CGO_ENABLED=0`. macOS shared driver built. The Linux
  CUDA and C Device C sources compiled with `-Wall -Wextra -Werror` in a local
  disposable Linux container.
- Complete Linux shared-driver linking and final macOS embedded C-archive
  linking passed. The public C Device header also compiled as C++. CI now runs
  the CPU experiment gates and native C Device CPU ownership/binding tests;
  GPU cases remain explicit hardware-only checks.
- Isolated local Redis Software, 3 shards, authenticated TLS/RESP3, active verified
  4× QPF was provisioned with current main's `make -C go redis-up`. Native CPU
  C Device tests passed: four tests, six GPU-only skips. Native BindDevice
  ingestion preserved NULLs and integers above 2^53 after the source was released.
- A stronger regression releases both caller buffers and schema between
  BindDevice and ExecuteQuery. It exposed Arrow Go's schema-consuming import;
  temporary schema views now isolate that importer, and the regression passes
  while leaving the caller descriptors untouched. Producer-event waits now poll
  with the statement cancellation context; their real-GPU cancellation test is
  provided separately.
- Direct Arrow stream consumption reproduced a pre-existing C stream release
  contract failure: the release callback did not clear `stream.release`, causing
  Arrow's lifecycle assertion to abort. The callback now clears it; the same
  native binding test passes without bypassing Arrow's assertion.
- Real-GPU tests for this native implementation are **not yet run**. A new L4
  EC2 fixture was launched, but an external IAM-user TerminateInstances API call
  ended it before SSM delivered the build/test commands. The user has been asked
  to coordinate the competing EC2 cleanup. Earlier cuDF prototype tests do not
  substitute for native CUDA validation.

The new API/SQL subset and ownership rules are documented in
[gpu-execution.md](../../../gpu-execution.md). The PR must remain draft until
the native GPU producer/consumer, CUDA SQL and early-release tests run on real
hardware. No native query speedup is claimed.
