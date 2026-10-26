# QPF versus adaptive client workers — October 22, 2026

The recommendation from this focused experiment is to keep client worker
parallelism and shared admission, and make adaptive tuning an opt-in choice.
QPF increases Redis Search capacity; it does not replace client concurrency or
guarantee that the adaptive policy finds the best allocation.

## Results

All 48 policy trials completed, yielding 144 concurrency records and 26,208
timed executions including learning. Every result matched an independent serial
executor. Fixed four-worker execution was the fastest fixed policy in all 12
cases. Adaptive tuning passed 7/12 predeclared gates: Standard 3/3, 2x 2/3,
4x 1/3, and 8x 1/3. Failures were throughput/tail regressions, not wrong results.
See [the comparison table](REPORT.md) and each factor's full gate/spread report.

For one concurrent statement, median QPS was:

| QPF | Fixed four workers | Adaptive | Adaptive/fixed |
|---|---:|---:|---:|
| Standard | 18.60 | 17.37 | 0.934 |
| 2x | 22.29 | 17.17 | 0.770 |
| 4x | 22.80 | 19.84 | 0.870 |
| 8x | 21.41 | 16.56 | 0.773 |

At QPF 4x, this fixed policy's single-client throughput was about 23% higher
than at Standard. Higher QPF was not monotonically faster. Allocation
histories show adaptive trials holding different retained worker counts;
holding is not evidence of an optimal allocation. This is evidence for fixed
four workers on this workload, not a universal recommendation of four workers.

## Conditions and limits

- Native Linux arm64 Go 1.27.2 test binary, CPU-limited client (2 CPUs, 2 GiB).
- Dedicated local Redis Software image `redislabs/redis:8.2.0-78.18`, one shard,
  Redis 8.6.2 / Search 8.6.11, 1 GiB RAM database, no replication/persistence.
- The same 16-CPU server ceiling for all factors. Effective QPF, Search workers
  (0/3/6/12), routing, and connection settings verified before/after every trial.
- 32,768 deterministic rows; `SUM(v), AVG(f), MIN(v), MAX(v)`; aggregate pushdown
  disabled to exercise the client aggregate worker policy. Search scans remain.
- Serial per-query workers/shared budget 1/2; CPU-fixed 2/2; headroom-fixed 4/4;
  adaptive auto/auto. Shared database handle across 1/2/4 concurrent statements.
- Three fresh-client repetitions; 60 learning and 18 measured rounds per
  concurrency. Synchronized arrivals, direct connection. Policies shuffled within
  factor; factors blocked in order 2x, Standard, 8x, 4x.
- Warm gates: >=90% throughput and <=115% p95 of the same highest-throughput
  fixed policy. Cold gates: >=85% throughput and <=120% p95 of CPU-fixed.

This local synthetic aggregate does not establish production sizing, maximum
QPF gains, general autotuning readiness, or partition throughput. Three
repetitions and coarse p95 windows leave uncertainty; host/time/order effects
remain possible. No application database was used.

## Statement and partition APIs

After timing, six existing top-level tests ran at every factor: statement
streaming, exact aggregate worker cleanup, deferred partition reads, capped
partition ranges, incremental discovery lifecycle, and retained publication,
replay and revoke. All 24 top-level invocations passed, with 112 passing test
entries including subtests and zero skips. See `api-checks.json` and
`api-qpf-*.log` for settings and output.

The initial retained check lacked Software's required external control-key
persistence declaration. Its failed setup attempt is preserved separately.
The rerun verified `noeviction` through the owned management API and set
`REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed`, matching CI; all six tests then
passed at every QPF. No driver changes were needed.

QPF applies transparently to Search work executed by statements and deferred
partition readers. Deferred `ExecutePartitions` returns descriptors; callers
schedule `ReadPartition` tasks. The aggregate autotuner does not schedule those
readers. Retained execution captures once with per-query workers set to one;
retained reads fetch saved Arrow chunks without rerunning Search. See
[the API explanation](../../../docs/worker-autotuning.md#query-performance-factor-and-the-adbc-apis).

## Reproduction

Start a dedicated Software deployment using `go/software-setup.sh` with unique
ports/project, allocate 16 server CPUs, and compile the native static Linux test
binary. With that deployment's names, run:

```sh
python3 benchmarks/worker-autotune/controlled/qpf_matrix.py \
  --output /tmp/qpf-comparison-new --binary /tmp/adbc-qpf-profile \
  --network adbc-qpf-autotune_default \
  --server-container adbc-qpf-autotune-redis-software-1 \
  --uri redis://redis-software:12000/0 --api-port 29443 \
  --cpu 2 --rows 32768 --warmup 60 --rounds 18 --repetitions 3 --modes waves
```

Each factor preserves its immutable manifest, gate report, compressed raw
results and trial logs. Harness copies preserve the measured version. The
earlier partial 4x-only experiment is retained in `../qpf-2026-10-22` and marked
cancelled/superseded; it is excluded from these comparisons.
