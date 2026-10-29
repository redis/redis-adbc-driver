# Redis ADBC v0.0.18: slide benchmark refresh

Measured October 29, 2026. Requested slides 8–15 rerun; slides 16–18 remain historical by user request. This is a source build of the exact v0.0.18 driver, not a full-scale ClickBench rerun.

Driver and Python source match public v0.0.18 commit `2f053d3affad90a1d2798c000f0d1825183e6e54`. ADBC GetInfo returns `v0.0.18`. Library SHA-256: `b0f0da0f9d1390c65662711112747a4d72553cd608965635fb8caade9e2770ab`.

macOS 26.7.2 arm64, 18 logical CPUs; Go 1.27.2; Python 3.13.16; PyArrow 26.0.0; ADBC manager/PostgreSQL driver 1.12.0; redis-py 8.1.0; hiredis 3.5.0. Dedicated local Redis Software 8.6.2 / Search 8.6.11, one shard, RESP3, no TLS or persistence, verified active 4× QPF/six Search workers/mnp/32 connections except the intentional QPF sweeps. Shared workstation, not an isolated performance lab.

All within-run client comparisons use the same data and server. Several historical charts used Redis Community or different host/toolchain limits: changes versus those charts cannot be attributed solely to the driver version. Observed minimum–maximum ranges are not confidence intervals.

The independently typed wide fixture exactly matches the original source hashes: Parquet `d053397f7aa5b2e1ee9160779a9e9307b162c6994aebba829c7ea1411076c4d0`; Arrow file `a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2`. It contains 100,000 rows × 105 columns and 79,578,019 logical Arrow bytes.

## Slide 8 — bulk loading

All 28 loads validated (four warmups plus 24 measured). Timer: common empty-table/index setup through fresh workers, input conversion, all writes, teardown and indexing readiness. Same Arrow input and 1,000-command pipelines. Redis server cap: four CPUs/8 GiB; database limit 2 GiB.

| Client | Writers | Median s | Range s | Rows/s | Peak worker RSS MiB |
|---|---|---|---|---|---|
| adbc | 1 | 7.841 | 7.493–9.031 | 12,754 | 724.7 |
| redis_py | 1 | 9.816 | 9.553–11.554 | 10,187 | 215.8 |
| adbc | 4 | 7.991 | 6.641–9.735 | 12,513 | 625.3 |
| redis_py | 4 | 7.217 | 6.812–8.501 | 13,856 | 213.2 |

One-writer ADBC takes 20.1% less elapsed time, with separated observed ranges. Four-writer ranges overlap; redis-py has the lower median. Native fixed-schema writes omit general SQL constraints/catalog fencing. ADBC uses more process memory.

## Slides 9–10 — incremental reads

All 98 executions validated (14 warmups plus 84 measured). Timer includes spawn/imports, connections, planning, full typed conversion, IPC output, close and teardown. First batch is a complete producer RecordBatch before IPC. Four readers consume eight tasks. Native references depend on immutable dense IDs/known schema. Four server CPUs, 8 GiB container, 2 GiB database limit.

| Path | Readers | Batch rows | First batch s | Full result s | Full range s |
|---|---|---|---|---|---|
| adbc | 1 | 512 | 0.383 | 3.704 | 3.639–3.828 |
| redis_py_known | 1 | 512 | 0.269 | 3.609 | 3.547–3.709 |
| redis_py_discovery | 1 | 512 | 0.274 | 3.837 | 3.795–3.960 |
| adbc_partitions | 4 | 512 | 0.470 | 1.870 | 1.699–1.882 |
| adbc_ranges | 4 | 512 | 0.453 | 1.853 | 1.821–2.090 |
| redis_py_known | 4 | 512 | 0.364 | 1.720 | 1.642–1.815 |
| redis_py_discovery | 4 | 512 | 0.371 | 1.856 | 1.732–1.929 |
| adbc | 1 | 4096 | 0.378 | 3.545 | 3.458–3.664 |
| redis_py_known | 1 | 4096 | 0.288 | 3.459 | 3.360–3.517 |
| redis_py_discovery | 1 | 4096 | 0.298 | 3.680 | 3.586–3.752 |
| adbc_partitions | 4 | 4096 | 0.475 | 1.790 | 1.701–1.954 |
| adbc_ranges | 4 | 4096 | 0.445 | 1.722 | 1.655–1.989 |
| redis_py_known | 4 | 4096 | 0.389 | 1.694 | 1.572–1.765 |
| redis_py_discovery | 4 | 4096 | 0.402 | 1.783 | 1.733–2.036 |

For the visible 4,096-row batch ceiling, ADBC one-reader full-result range overlaps each native path; redis-py reaches the first batch earlier. All four-reader full-result ranges overlap. This supersedes the old one-reader speed claim for the new Software experiment, not as proof of a driver-version regression.

## Slide 11 — UNION ALL peak process memory

All 36 fresh-process executions independently validated 3,840,000 output rows. Same v0.0.18 binary toggles streaming. This table selects N distinct keys; both 1 and N cardinalities were tested. Four server CPUs/8 GiB, 1 GiB database limit. RSS includes Python, Go, Arrow and validation; 1024-row streaming batches versus 65536-row materialized batches. No isolated timing claim.

| Output rows | Materialized MiB | Streamed MiB | RSS reduction |
|---|---|---|---|
| 20,000 | 110.2 | 91.0 | 17.4% |
| 100,000 | 240.4 | 101.4 | 57.8% |
| 200,000 | 382.0 | 100.5 | 73.7% |

## Slide 12 — native IPC versus PyArrow

All 144 operations validated, including 24 warmups and 120 measured fresh processes; 7,202,328 output/read-back rows checked. All 12 observed time ranges overlap. Same one-worker ADBC source, data, 4096-row batches and local uncompressed files; SQL/conversion/file close included, connection setup excluded, no fsync. Imports do not include an index-readiness boundary.

| Direction / workload | Format | Native seconds: median (range) | PyArrow seconds: median (range) | Native elapsed change |
|---|---|---:|---:|---:|
| export / aggregate-narrow | file | 0.2038 (0.1987–0.2140) | 0.2031 (0.1964–0.2157) | +0.3% |
| export / aggregate-narrow | stream | 0.2048 (0.2015–0.2068) | 0.2074 (0.2020–0.2167) | -1.3% |
| export / scan-narrow | file | 0.2581 (0.2483–0.2679) | 0.2583 (0.2540–0.2654) | -0.1% |
| export / scan-narrow | stream | 0.2733 (0.2663–0.3116) | 0.2724 (0.2625–0.2761) | +0.3% |
| export / scan-wide | file | 1.1961 (1.1882–1.2121) | 1.1871 (1.1505–1.2236) | +0.8% |
| export / scan-wide | stream | 1.1946 (1.1854–1.2206) | 1.1810 (1.1692–1.2144) | +1.2% |
| export / union-wide | file | 2.4791 (2.4559–2.5125) | 2.4885 (2.4728–2.5702) | -0.4% |
| export / union-wide | stream | 2.4467 (2.4097–2.4955) | 2.4523 (2.4332–2.4834) | -0.2% |
| import / import-narrow | file | 0.2085 (0.2028–0.2131) | 0.2052 (0.2030–0.2116) | +1.6% |
| import / import-narrow | stream | 0.2200 (0.2089–0.2321) | 0.2181 (0.2033–0.2377) | +0.9% |
| import / import-wide | file | 1.1997 (1.1910–1.2462) | 1.2116 (1.1932–1.2763) | -1.0% |
| import / import-wide | stream | 1.2438 (1.2033–1.4628) | 1.2503 (1.2078–1.3552) | -0.5% |

Negative elapsed change means a lower native median; 12/12 elapsed ranges overlap.

| Workload / format | Method | Output rows/s | IPC MiB/s | Client CPU seconds | Peak RSS MiB | Encoded MiB |
|---|---|---:|---:|---:|---:|---:|
| aggregate-narrow / file | native | 476 | 0.02 | 0.1340 | 97.8 | 0.0033 |
| aggregate-narrow / file | pyarrow | 478 | 0.02 | 0.1411 | 98.7 | 0.0033 |
| aggregate-narrow / stream | native | 474 | 0.01 | 0.1315 | 96.9 | 0.0031 |
| aggregate-narrow / stream | pyarrow | 468 | 0.01 | 0.1355 | 96.6 | 0.0031 |
| scan-narrow / file | native | 193,712 | 10.03 | 0.1307 | 98.3 | 2.5888 |
| scan-narrow / file | pyarrow | 193,555 | 10.02 | 0.1331 | 96.5 | 2.5888 |
| scan-narrow / stream | native | 182,948 | 9.47 | 0.1345 | 99.4 | 2.5882 |
| scan-narrow / stream | pyarrow | 183,547 | 9.50 | 0.1391 | 99.4 | 2.5882 |
| scan-wide / file | native | 41,803 | 33.89 | 0.7334 | 170.3 | 40.5357 |
| scan-wide / file | pyarrow | 42,121 | 34.15 | 0.7389 | 167.0 | 40.5352 |
| scan-wide / stream | native | 41,854 | 33.93 | 0.7331 | 162.0 | 40.5339 |
| scan-wide / stream | pyarrow | 42,336 | 34.32 | 0.7375 | 171.5 | 40.5334 |
| union-wide / file | native | 40,338 | 32.70 | 1.6417 | 211.8 | 81.0662 |
| union-wide / file | pyarrow | 40,184 | 32.58 | 1.6636 | 214.2 | 81.0651 |
| union-wide / stream | native | 40,871 | 33.13 | 1.6442 | 208.8 | 81.0641 |
| union-wide / stream | pyarrow | 40,778 | 33.06 | 1.6619 | 213.9 | 81.0630 |
| import-narrow / file | native | 239,807 | 12.42 | 0.0785 | 75.6 | 2.5891 |
| import-narrow / file | pyarrow | 243,641 | 12.62 | 0.0781 | 76.9 | 2.5891 |
| import-narrow / stream | native | 227,316 | 11.77 | 0.0801 | 76.5 | 2.5886 |
| import-narrow / stream | pyarrow | 229,299 | 11.87 | 0.0816 | 77.7 | 2.5886 |
| import-wide / file | native | 41,677 | 33.80 | 0.4724 | 148.9 | 40.5449 |
| import-wide / file | pyarrow | 41,267 | 33.46 | 0.4623 | 145.9 | 40.5449 |
| import-wide / stream | native | 40,200 | 32.60 | 0.4885 | 148.8 | 40.5431 |
| import-wide / stream | pyarrow | 39,992 | 32.43 | 0.4830 | 146.5 | 40.5431 |

## Slide 13 — concurrent indexed query throughput

All 90 trials completed with zero errors, three rounds per factor/query/client-count combination. Five-second request windows plus drain; full reference comparisons after each factor change and row-count checks inside timing. Standard/2×/4× order rotates. Server cap four CPUs/8 GiB, 2 GiB database limit. Standard means active factor zero and verified zero Search workers/cmp/five connections; 2×/4× verify three/six workers/mnp/32. Restores 4×.

| Query / clients | Standard QPS | 2× QPS | 4× QPS | 4× / Standard |
|---|---|---|---|---|
| index_group / 16 | 7.96 | 10.95 | 11.78 | 1.48× |
| index_count / 16 | 294.60 | 740.50 | 749.31 | 2.54× |
| full_scan / 1 | 1.31 | 1.26 | 1.27 | 0.96× |

The legacy index_group label means GROUP BY + SUM, not pure server pushdown. A separate untimed diagnostic confirmed partitioned_exact execution: two workers fetched 100,000 source rows and the driver merged the result. Diagnostic timing is excluded. Indexed range COUNT uses a different execution path.

## Slide 14 — QPF and adaptive workers

All 48 fresh-client policy trials completed, producing 144 concurrency records. Each result checked against independent serial execution. One 32768-row mixed aggregate with pushdown disabled; factors Standard/2×/4×/8×; serial/fixed-2/fixed-4/adaptive policies; 1/2/4 simultaneous statements; three repetitions; 60 learning and 18 measured rounds. Linux arm64 client two CPUs/2 GiB, server 16 CPUs/8 GiB, database limit 2 GiB. Factor blocks 2×, Standard, 8×, 4×; policy order shuffled within blocks.

Adaptive policy passed 10 of 12 predeclared combined cold/warm performance gates. These are throughput/tail-latency results, not functional test failures.

| QPF | Concurrency | Best fixed policy | Fixed QPS | Adaptive QPS | Warm QPS ratio | Combined gate |
|---|---|---|---|---|---|---|
| Standard | 1 | headroom-fixed | 17.42 | 17.20 | 0.987 | Pass |
| Standard | 2 | headroom-fixed | 17.14 | 16.72 | 0.976 | Pass |
| Standard | 4 | headroom-fixed | 17.89 | 19.39 | 1.083 | Pass |
| 2 | 1 | headroom-fixed | 21.56 | 20.21 | 0.938 | Pass |
| 2 | 2 | headroom-fixed | 22.00 | 21.10 | 0.959 | Pass |
| 2 | 4 | headroom-fixed | 22.91 | 22.84 | 0.997 | Pass |
| 4 | 1 | headroom-fixed | 19.38 | 17.78 | 0.917 | Pass |
| 4 | 2 | serial | 19.62 | 18.18 | 0.926 | Pass |
| 4 | 4 | headroom-fixed | 19.76 | 20.16 | 1.020 | Pass |
| 8 | 1 | headroom-fixed | 18.44 | 16.22 | 0.880 | Fail |
| 8 | 2 | headroom-fixed | 20.28 | 14.56 | 0.718 | Fail |
| 8 | 4 | headroom-fixed | 19.86 | 19.51 | 0.982 | Pass |

Warm thresholds: at least 90% QPS and at most 115% p95 of the same best fixed policy. Cold thresholds: at least 85% QPS and at most 120% p95 of fixed-2. Three repetitions and coarse p95 leave uncertainty. This is not an optimum guarantee or a generic sizing recommendation.

## Slide 15 — matched cross-database wide transfer

All 72 executions validated (12 warmups plus 60 measured); exact values and multiplicity match the same typed source. PostgreSQL 16.10/ADBC COPY and ClickHouse 25.8.33.6/ArrowStream. Every server container is limited to four CPUs/8 GiB; Redis database limit 2 GiB. Reused readers keep connections open; fresh readers include spawn/import/connect/close. All paths normalize full Arrow IPC output. Redis uses public partitions for four readers; references use explicit ordinal ranges. No traffic remeasurement or equal-cost/product-wide claim.

| Engine | Lifecycle | Readers | Median s | Range s |
|---|---|---|---|---|
| redis | persistent | 1 | 3.294 | 3.203–3.353 |
| redis | persistent | 4 | 1.530 | 1.406–1.648 |
| redis | spawned | 1 | 3.619 | 3.390–4.065 |
| redis | spawned | 4 | 1.785 | 1.647–2.293 |
| postgres | persistent | 1 | 0.345 | 0.331–0.384 |
| postgres | persistent | 4 | 0.199 | 0.185–0.208 |
| postgres | spawned | 1 | 0.593 | 0.559–0.653 |
| postgres | spawned | 4 | 0.459 | 0.430–0.634 |
| clickhouse | persistent | 1 | 0.206 | 0.171–0.221 |
| clickhouse | persistent | 4 | 0.148 | 0.140–0.180 |
| clickhouse | spawned | 1 | 0.374 | 0.332–0.541 |
| clickhouse | spawned | 4 | 0.410 | 0.341–0.586 |

## Full-scale scope

Slides 16–18 were explicitly skipped by the user for this refresh. Their historical Redis Community 99,997,497-row numbers remain dated and are not v0.0.18/Redis Software evidence. No paid deployment was provisioned.

The measurements here retain the fields required to audit the comparisons. Operational manifests, endpoint details, deployment identifiers, credentials, complete process logs, binaries and slide-editing payloads remain private. Production driver code was not modified. See [reproduction and audit](README.md).
