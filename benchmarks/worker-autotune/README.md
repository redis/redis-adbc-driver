# Worker autotuning evaluation — October 9, 2026

The tuned policy adapts during normal execution, but this short matrix does not establish maximum performance or a universal regression bound. Cold learning, noisy measurements, and bounded probes sometimes underperform fixed settings. Treat these as local evaluation results, not a production performance guarantee. Significant observed regressions leave this implementation in draft; default performance readiness requires further tuning and controlled, longer validation.

Environment: macOS arm64, Apple M4, Go 1.26.6, isolated Docker Redis Open Source 8.6.2 with Search. The workstation and Docker runtime are shared; the Go CPU capacities constrain the client runtime, not the Redis server. Results are one ordered run without randomized repetition. Redis Software and production-scale datasets were not measured in this run.

Workload: 8,192 rows, 30 rounds per case, `SELECT SUM(v), AVG(f), MIN(v), MAX(v)` with aggregate pushdown disabled. Serial uses one worker per query with a shared budget equal to CPU capacity. Fixed uses twice CPU capacity for both the query ceiling and shared budget. Tuned leaves both settings automatic. Each database handle is reused across the 1/2/4 concurrency phases; workload/concurrency changes restart learning. The impaired link delays each forwarded server-read chunk by 2 ms; this is not precise RTT emulation.

The optimization objective and decision thresholds are defined in [worker-autotuning.md](../../docs/worker-autotuning.md). The table includes cold learning and all probes. The p95 columns are measured query percentiles; the tuner itself uses a smoothed cohort maximum as its conservative tail signal.

| Link | CPU | Queries | Serial q/s | Fixed q/s | Tuned q/s | Tuned vs best fixed/serial | Serial p95 ms | Fixed p95 ms | Tuned p95 ms | Final tuned budget | Budget changes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| direct | 1 | 1 | 15.09 | 18.27 | 11.73 | -35.8% | 143.7 | 75.8 | 170.3 | 2 | 9 |
| direct | 1 | 2 | 18.90 | 20.17 | 12.21 | -39.5% | 137.3 | 117.8 | 265.2 | 2 | 5 |
| direct | 1 | 4 | 19.16 | 20.36 | 14.95 | -26.6% | 236.8 | 231.2 | 341.6 | 1 | 8 |
| direct | 2 | 1 | 16.49 | 17.86 | 21.78 | +22.0% | 87.2 | 84.2 | 66.4 | 1 | 7 |
| direct | 2 | 2 | 20.11 | 22.07 | 24.88 | +12.7% | 139.5 | 116.5 | 93.7 | 2 | 6 |
| direct | 2 | 4 | 16.11 | 25.57 | 21.10 | -17.5% | 331.8 | 193.1 | 240.2 | 2 | 4 |
| direct | 4 | 1 | 19.64 | 29.98 | 14.73 | -50.9% | 86.1 | 48.9 | 116.0 | 3 | 9 |
| direct | 4 | 2 | 24.32 | 26.59 | 23.17 | -12.9% | 109.3 | 102.4 | 114.6 | 1 | 9 |
| direct | 4 | 4 | 30.87 | 35.17 | 28.46 | -19.1% | 163.7 | 133.5 | 171.4 | 4 | 6 |
| impaired | 1 | 1 | 10.27 | 15.77 | 12.44 | -21.2% | 121.4 | 82.4 | 106.5 | 2 | 7 |
| impaired | 1 | 2 | 11.05 | 16.61 | 14.77 | -11.1% | 193.0 | 143.8 | 170.7 | 2 | 5 |
| impaired | 1 | 4 | 12.26 | 16.44 | 14.02 | -14.7% | 338.6 | 259.1 | 337.9 | 2 | 5 |
| impaired | 2 | 1 | 9.78 | 12.78 | 12.90 | +1.0% | 159.1 | 135.2 | 114.9 | 2 | 8 |
| impaired | 2 | 2 | 13.82 | 14.34 | 13.00 | -9.4% | 170.1 | 157.3 | 200.0 | 2 | 8 |
| impaired | 2 | 4 | 17.39 | 19.77 | 19.48 | -1.4% | 257.8 | 233.4 | 256.9 | 3 | 7 |
| impaired | 4 | 1 | 11.05 | 24.84 | 21.67 | -12.8% | 106.7 | 50.2 | 66.8 | 4 | 6 |
| impaired | 4 | 2 | 18.40 | 25.96 | 23.58 | -9.2% | 125.6 | 94.6 | 96.1 | 3 | 7 |
| impaired | 4 | 4 | 28.36 | 33.69 | 18.82 | -44.2% | 181.1 | 135.4 | 276.6 | 2 | 8 |

Across these cases, tuned throughput versus the better baseline ranges from -50.9% to +22.0%. Measured p95 relative to the better baseline ranges from -21.1% to +137.2%. The internal 10% throughput / 15% tail decision thresholds therefore do not imply equivalent bounds against independently chosen fixed configurations.

Convergence is workload dependent: the final budget can still be a probe. Budget changes count transitions across all 30 rounds, not just accepted improvements. Each candidate receives three comparable observations; four stable windows separate later probes. The raw allocation history contains every round, grant, signal, and reason, including regime resets and rollbacks. Mixed traffic is deliberately conservative. Longer runs and randomized repetitions across larger datasets and Redis deployments are needed before making broad performance claims.

[metrics.csv](metrics.csv) reports every case’s latency, throughput, client CPU seconds/utilization, peak Go heap, allocated bytes, network bytes/sec, Redis commands/sec, Redis CPU seconds, and Redis memory. Redis counters cover the entire isolated server; heap sampling is every 5 ms. Allocation snapshots and full convergence evidence are in [results.jsonl](results.jsonl). [overhead.txt](overhead.txt) reports the separate local telemetry/cohort microbenchmarks; these exclude SQL execution, so the profile comparisons provide the practical end-to-end cost.

Reproduce with:

```sh
cd go
go test -c -o /tmp/redis-tuner.test .
cd ..
python3 benchmarks/worker-autotune/matrix.py --binary /tmp/redis-tuner.test --uri redis://127.0.0.1:16379/0 --rows 8192 --rounds 30 --output benchmarks/worker-autotune/results.jsonl
python3 benchmarks/worker-autotune/summarize.py
```
