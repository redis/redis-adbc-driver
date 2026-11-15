# Native point-read target screen (limited selection)

96 validated records across A1/B1/B2/A2: six point-read cases/window, 24 warmups and 72 measured records. **Not a full CRUD/query matrix.** Target: **NOT MET**.

Pooled measured operation p50/mean, nearest-rank p95 and finite summed-trial throughput. Ratios use unrounded samples/statistics; raw Redis controls remain separate. Lazy first-operation transport work remains included; no capacity/confidence/causal CPU claim.

## Pooled

| Mode | Clients | P50 ms B→C | P95 ms B→C | Mean ms B→C | P50 less elapsed | Finite ops/s B→C | C / redis-py |
|---|---:|---:|---:|---:|---:|---:|---:|
| managed | 1 | 2.0227→1.8318 | 2.7677→2.6442 | 2.1012→1.9388 | 9.44% | 475.69→515.49 | 33.818× |
| managed | 4 | 4.9958→4.5582 | 8.7634→8.5781 | 5.3983→5.0291 | 8.76% | 720.28→765.68 | 14.241× |
| redis_py | 1 | 0.0524→0.0542 | 0.1335→0.1542 | 0.1211→0.1260 | -3.38% | 8202.63→7854.31 | 1.000× |
| redis_py | 4 | 0.3580→0.3201 | 4.2998→4.3725 | 0.8243→0.7894 | 10.60% | 4487.84→4645.37 | 1.000× |
| unmanaged | 1 | 2.0176→1.8742 | 3.6122→3.3212 | 2.1177→1.9760 | 7.11% | 471.98→505.80 | 34.601× |
| unmanaged | 4 | 5.1265→4.8649 | 8.4326→9.2693 | 5.4650→5.2138 | 5.10% | 708.98→750.38 | 15.200× |

## Forward pair (baseline→candidate)

| Mode | Clients | P50 ms B→C | P95 ms B→C | Mean ms B→C | P50 less elapsed | Finite ops/s B→C | C / redis-py |
|---|---:|---:|---:|---:|---:|---:|---:|
| managed | 1 | 2.0309→1.8318 | 2.8404→2.5709 | 2.1228→1.9319 | 9.80% | 470.85→517.31 | 33.949× |
| managed | 4 | 4.9559→4.4495 | 8.6812→7.9335 | 5.3716→4.8403 | 10.22% | 721.81→801.24 | 12.851× |
| redis_py | 1 | 0.0526→0.0540 | 0.1339→0.1321 | 0.1238→0.1253 | -2.53% | 8024.83→7878.94 | 1.000× |
| redis_py | 4 | 0.3496→0.3462 | 4.1292→4.6358 | 0.8059→0.8060 | 0.95% | 4574.92→4497.73 | 1.000× |
| unmanaged | 1 | 1.9842→1.9092 | 3.5705→3.5332 | 2.0729→2.0496 | 3.78% | 482.19→487.63 | 35.383× |
| unmanaged | 4 | 5.2896→4.8236 | 8.4147→9.2693 | 5.6049→5.1182 | 8.81% | 688.70→760.80 | 13.932× |

## Reverse pair (baseline→candidate)

| Mode | Clients | P50 ms B→C | P95 ms B→C | Mean ms B→C | P50 less elapsed | Finite ops/s B→C | C / redis-py |
|---|---:|---:|---:|---:|---:|---:|---:|
| managed | 1 | 2.0152→1.8340 | 2.7677→3.1435 | 2.0796→1.9456 | 8.99% | 480.63→513.69 | 33.794× |
| managed | 4 | 5.0221→4.6053 | 8.9692→9.3254 | 5.4250→5.2179 | 8.30% | 718.75→733.14 | 16.006× |
| redis_py | 1 | 0.0522→0.0543 | 0.1047→0.1564 | 0.1184→0.1268 | -3.91% | 8388.49→7829.84 | 1.000× |
| redis_py | 4 | 0.3620→0.2877 | 4.7408→4.3725 | 0.8427→0.7727 | 20.51% | 4404.01→4803.02 | 1.000× |
| unmanaged | 1 | 2.0331→1.8530 | 3.8173→2.7967 | 2.1625→1.9023 | 8.86% | 462.20→525.39 | 34.143× |
| unmanaged | 4 | 5.0484→4.8851 | 8.5646→9.4926 | 5.3252→5.3093 | 3.23% | 730.50→740.25 | 16.978× |

