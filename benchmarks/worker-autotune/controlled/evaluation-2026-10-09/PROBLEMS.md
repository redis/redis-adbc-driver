# Problems observed in the stopped evaluation

The user stopped the run after 47/222 invocations, preserving 141/666 records. No full repeated comparison completed. These single-repetition observations motivate follow-up work; they establish neither causal attribution nor readiness.

The frozen measured source is `09588f0ca874a34cfbf092cf39ecdf06cbed6a0c`. The subsequent incomparable-probe fallback and cached-capacity diagnostic fixes were tested separately, not measured by this matrix.

## Provisional comparisons

Reference is the highest-throughput fixed policy collected in that same scenario and repetition. Only scenarios with all four policies collected are shown. Ratios are tuned/reference.

| Deployment / rows / CPU / link / arrivals / concurrency | Reference | QPS ratio | p95 ratio |
|---|---|---:|---:|
| oss / 32768 / 1 / direct / waves / 1 | cpu-fixed | 0.971 | 1.066 |
| oss / 32768 / 1 / direct / waves / 2 | cpu-fixed | 0.949 | 0.989 |
| oss / 32768 / 1 / direct / waves / 4 | serial | 0.984 | 1.047 |
| oss / 32768 / 2 / direct / waves / 1 | headroom-fixed | 1.035 | 0.979 |
| oss / 32768 / 2 / direct / waves / 2 | serial | 1.077 | 0.918 |
| oss / 32768 / 2 / direct / waves / 4 | headroom-fixed | 1.063 | 0.859 |
| oss / 32768 / 4 / aggregate-response-2ms / waves / 1 | headroom-fixed | 1.003 | 0.975 |
| oss / 32768 / 4 / aggregate-response-2ms / waves / 2 | headroom-fixed | 0.974 | 1.042 |
| oss / 32768 / 4 / aggregate-response-2ms / waves / 4 | serial | 0.856 | 1.096 |
| oss / 32768 / 4 / direct / waves / 1 | headroom-fixed | 0.993 | 1.004 |
| oss / 32768 / 4 / direct / waves / 2 | headroom-fixed | 1.011 | 0.984 |
| oss / 32768 / 4 / direct / waves / 4 | serial | 0.872 | 1.134 |
| oss / 100000 / 2 / direct / continuous / 1 | headroom-fixed | 1.015 | 0.965 |
| oss / 100000 / 2 / direct / continuous / 2 | serial | 0.925 | 1.023 |
| oss / 100000 / 2 / direct / continuous / 4 | headroom-fixed | 0.985 | 0.806 |
| oss / 100000 / 2 / direct / waves / 1 | headroom-fixed | 0.888 | 1.402 |
| oss / 100000 / 2 / direct / waves / 2 | headroom-fixed | 0.854 | 1.163 |
| oss / 100000 / 2 / direct / waves / 4 | headroom-fixed | 0.933 | 1.063 |
| software / 32768 / 1 / aggregate-response-2ms / waves / 1 | headroom-fixed | 0.971 | 1.157 |
| software / 32768 / 1 / aggregate-response-2ms / waves / 2 | headroom-fixed | 0.949 | 1.110 |
| software / 32768 / 1 / aggregate-response-2ms / waves / 4 | headroom-fixed | 0.897 | 1.218 |
| software / 32768 / 1 / direct / waves / 1 | headroom-fixed | 0.891 | 1.134 |
| software / 32768 / 1 / direct / waves / 2 | headroom-fixed | 0.914 | 1.060 |
| software / 32768 / 1 / direct / waves / 4 | headroom-fixed | 0.891 | 1.175 |
| software / 32768 / 2 / direct / server-transition / 1 | cpu-fixed | 0.952 | 1.041 |
| software / 32768 / 2 / direct / server-transition / 2 | headroom-fixed | 0.903 | 1.196 |
| software / 32768 / 2 / direct / server-transition / 4 | serial | 1.015 | 0.900 |
| software / 32768 / 2 / direct / waves / 1 | headroom-fixed | 0.930 | 1.081 |
| software / 32768 / 2 / direct / waves / 2 | serial | 0.878 | 1.169 |
| software / 32768 / 2 / direct / waves / 4 | headroom-fixed | 0.960 | 1.103 |
| software / 100000 / 2 / direct / waves / 1 | headroom-fixed | 1.052 | 0.942 |
| software / 100000 / 2 / direct / waves / 2 | headroom-fixed | 1.026 | 1.018 |
| software / 100000 / 2 / direct / waves / 4 | serial | 1.140 | 0.846 |

## Remaining problems

- Some collected cases fall outside the planned warmed throughput or tail bounds. A held allocation is not proof of an optimal worker count. Even equal worker targets can have different trial timings; host activity, GC, throttling, and sampling variability remain possible contributors. See allocation histories before attributing a difference to the tuner.
- Larger inputs and mixed/continuous arrivals still require complete repeated coverage. CPU-capacity transition coverage and three-repetition uncertainty remain incomplete.
- The frozen runner did not finalize reports on an operator interrupt. This PR adds cancellation reporting; the stopped artifact was finalized from the closed gzip and unchanged manifest.
- Runtime safety fixes restore retained allocation after eight incomparable probe/verification cohorts and correct cached-runtime-capacity reasons. They do not provide a wall-clock or external performance bound.

Issues #285 and #286 remain open. `REPORT.md` lists missing coverage; `ALLOCATION.md` describes the collected allocation snapshots. Raw logs are archived in `trial-logs.tar.gz`; the interrupted invocation has no completed record. Full-suite logs precede the last guard changes; mixed-safety logs exercise the final source separately.
