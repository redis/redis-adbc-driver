# Controlled autotuning evaluation

Gate status: **pass**

Medians and full min/max spread across independent repeated trials; these are not confidence intervals. Warmed windows can still include probes. Local allocation is not a global optimum guarantee.

Cold bounds: QPS >= 85% and p95 <= 120% of the CPU-sized baseline. Warmed bounds: QPS >= 90% and p95 <= 115% of the highest-throughput measured fixed policy.

| Deployment / rows / CPU / link / arrivals / concurrency | Fixed reference | Warm QPS ratio | Warm p95 ratio | Cold QPS ratio | Cold p95 ratio | Pass |
|---|---|---:|---:|---:|---:|---|
| software / 32768 / 2 / direct / waves / 1 | headroom-fixed | 0.934 | 0.994 | 0.973 | 1.020 | True |
| software / 32768 / 2 / direct / waves / 2 | headroom-fixed | 0.946 | 1.060 | 1.038 | 0.935 | True |
| software / 32768 / 2 / direct / waves / 4 | headroom-fixed | 0.956 | 1.070 | 1.124 | 0.973 | True |
