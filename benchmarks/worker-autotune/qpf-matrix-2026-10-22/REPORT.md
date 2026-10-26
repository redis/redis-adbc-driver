# QPF and worker policy comparison

Tuned / highest-throughput fixed policy, using medians across repeated trials. QPF is blocked in order 2x, Standard, 8x, 4x; residual time/order effects are possible.

| QPF | Arrivals | Concurrent statements | Best fixed | Tuned QPS | Fixed QPS | QPS ratio | p95 ratio | Gate |
|---|---|---:|---|---:|---:|---:|---:|---|
| Standard | waves | 1 | headroom-fixed | 17.37 | 18.60 | 0.934 | 0.994 | True |
| Standard | waves | 2 | headroom-fixed | 17.98 | 19.01 | 0.946 | 1.060 | True |
| Standard | waves | 4 | headroom-fixed | 19.14 | 20.03 | 0.956 | 1.070 | True |
| 2 | waves | 1 | headroom-fixed | 17.17 | 22.29 | 0.770 | 1.279 | False |
| 2 | waves | 2 | headroom-fixed | 21.44 | 20.93 | 1.024 | 0.943 | True |
| 2 | waves | 4 | headroom-fixed | 23.74 | 21.74 | 1.092 | 0.939 | True |
| 4 | waves | 1 | headroom-fixed | 19.84 | 22.80 | 0.870 | 1.328 | False |
| 4 | waves | 2 | headroom-fixed | 19.32 | 23.03 | 0.839 | 1.209 | False |
| 4 | waves | 4 | headroom-fixed | 22.73 | 24.37 | 0.933 | 1.103 | True |
| 8 | waves | 1 | headroom-fixed | 16.56 | 21.41 | 0.773 | 1.216 | False |
| 8 | waves | 2 | headroom-fixed | 18.86 | 22.28 | 0.846 | 1.270 | False |
| 8 | waves | 4 | headroom-fixed | 22.81 | 22.77 | 1.002 | 1.005 | True |

One mixed aggregate, direct connection. See each factor manifest for row counts, server topology, client/server limits, repetitions, and phase lengths. P95 is coarse at low concurrency. This is a local policy comparison, not general readiness or partition throughput evidence. See each factor manifest, full min/max spreads, allocation histories and correctness logs.
