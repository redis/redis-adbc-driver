# Redis ADBC v0.0.25 local performance refresh

Measured November 12, 2026. The user canceled the 100-million-row ClickBench rerun; the three historical full-scale slides remain unchanged. No full-scale reload, cloud deployment or new Flex performance test was performed.

Exact release source: `a8de1c3cce12138ae4c37cdbff1e8ed8875b3bc4`. ADBC GetInfo reports `v0.0.25`. Library SHA-256: `cb605995b00241616f0b354d3452ce64c18fbd7063ebad1c23ac71db8203134f`. The independently typed 100K × 105 Arrow fixture is the same prior-run file, SHA-256 `a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2`.

Redis Software 8.2.6-25 / Redis 8.8.0 / Search 8.8.8, one RAM shard. Active 4× QPF, six Search workers, mnp routing and 32 connections were verified before timing throughout. Server: four CPUs/eight GiB; database: four GiB/noeviction/no persistence. Localhost authenticated, certificate-verified TLS and RESP3. Apple M5 Max macOS arm64, 18 logical CPUs/36 GiB RAM; Go 1.27.3; Python 3.15.2; PyArrow 26.0.1; ADBC manager and PostgreSQL driver 1.13.0; redis-py 8.1.0/hiredis 3.5.0. Independent suites ran sequentially on a shared workstation.

These are within-run workload comparisons. The older deck used a different server, TLS setting, toolchain and mutable storage layout. Differences from October cannot be attributed solely to the driver version. Observed min–max ranges are not confidence intervals.

## Loading

All 16 loads validated every typed value and multiplicity: one warmup and three measured rounds per client/writer count. Same 100K × 105 Arrow values; ADBC uses public managed-table append calls of up to 10K rows. Native uses a separate fixed-schema HASH layout, preassigned dense IDs, 1,000-command HSET pipelines and its own Search index. ADBC includes snapshot publication, version directories and compaction; native writes omit those guarantees. This replaces the legacy harness that wrote directly into mutable managed HASHes. It is not a single-publication bulk-ingestion benchmark.

Timer includes empty table/index setup, fresh worker startup/imports, Arrow input conversion, connections, writes, shutdown and index readiness. Independent validation, fixture preparation and cleanup are outside timing. Arrow nullability declarations were normalized only after checking the actual non-null values and identical column types.

| Client | Writers | Seconds: median (range) | Rows/s | Peak worker RSS MiB |
|---|---|---|---|---|
| adbc | 1 | 59.807 (59.646–60.289) | 1,672 | 449.0 |
| adbc | 4 | 139.384 (137.243–144.793) | 717 | 448.7 |
| redis_py | 1 | 10.531 (10.467–10.919) | 9,496 | 202.3 |
| redis_py | 4 | 7.712 (6.092–9.260) | 12,967 | 203.3 |

## Incremental reads

All 84 executions validated complete typed values and duplicate counts: 12 path/reader/batch cases, one warmup plus six measured repetitions. Managed ADBC and native read their own immutable fixtures with the same source values. Full-result timing includes fresh processes/imports, connections, planning, fetch/conversion, complete Arrow IPC output and shutdown. First batch is a typed producer RecordBatch before IPC. Four readers consume eight tasks. Native assumes known schema/dense IDs; ADBC provides a statement snapshot. The former explicit managed row-ID-range variant was not rerun; the four-reader table now shows the native known-key path instead.

| Path | Readers | Batch rows | First batch seconds | Full seconds: median (range) |
|---|---|---|---|---|
| adbc | 1 | 4096 | 0.519 | 3.477 (3.349–3.551) |
| adbc | 1 | 512 | 0.470 | 3.424 (3.324–3.928) |
| adbc_partitions | 4 | 4096 | 0.753 | 2.393 (2.039–3.280) |
| adbc_partitions | 4 | 512 | 0.463 | 2.155 (1.864–4.311) |
| redis_py_discovery | 1 | 4096 | 0.444 | 4.281 (3.825–7.138) |
| redis_py_discovery | 1 | 512 | 0.389 | 4.330 (4.291–4.755) |
| redis_py_discovery | 4 | 4096 | 0.707 | 2.576 (2.191–4.604) |
| redis_py_discovery | 4 | 512 | 0.565 | 2.374 (2.051–2.946) |
| redis_py_known | 1 | 4096 | 0.385 | 3.855 (3.558–5.092) |
| redis_py_known | 1 | 512 | 0.348 | 3.942 (3.748–4.765) |
| redis_py_known | 4 | 4096 | 0.630 | 2.476 (1.975–3.064) |
| redis_py_known | 4 | 512 | 0.540 | 2.118 (2.004–3.281) |

## UNION ALL memory

All 36 fresh-process executions validate every key, label and duplicate: three input sizes, 1/N distinct keys, streaming on/off, three repetitions. Same binary in both modes. Table selects N keys. RSS includes Python, Go, Arrow and validation; streaming uses 1,024-row batches, materialized uses the default batch size. No isolated timing or server-memory claim.

| Output rows | Materialized MiB | Streamed MiB | Less peak RSS |
|---|---|---|---|
| 20,000 | 120.5 | 97.1 | 19.4% |
| 100,000 | 252.0 | 106.3 | 57.8% |
| 200,000 | 386.1 | 108.1 | 72.0% |

## Native IPC

All 144 operations independently validated complete export or imported readback: one warmup plus five measured fresh processes per method/format/workload. Same data/query and one worker, 4,096-row batches, local uncompressed file/stream output without fsync. Timer includes SQL, conversion and file close; excludes connection setup and verification. Imports include managed snapshot publication.

| Workload | Format | Native seconds | PyArrow seconds | Native change | Ranges overlap |
|---|---|---|---|---|---|
| export/aggregate-narrow | file | 0.2022 (0.1844–0.2164) | 0.2019 (0.1806–0.2146) | +0.2% | True |
| export/aggregate-narrow | stream | 0.1884 (0.1765–0.2010) | 0.1811 (0.1743–0.1927) | +4.0% | True |
| export/scan-narrow | file | 0.2795 (0.2699–0.3435) | 0.2908 (0.2700–0.3260) | -3.9% | True |
| export/scan-narrow | stream | 0.2900 (0.2883–0.2987) | 0.3123 (0.2957–0.3441) | -7.1% | True |
| export/scan-wide | file | 1.1639 (1.1354–1.2069) | 1.1703 (1.1499–1.1738) | -0.6% | True |
| export/scan-wide | stream | 1.1492 (1.1350–1.1732) | 1.1521 (1.1456–1.1916) | -0.3% | True |
| export/union-wide | file | 3.2831 (2.6214–3.6684) | 2.7185 (2.5094–3.5734) | +20.8% | True |
| export/union-wide | stream | 2.4383 (2.3400–2.5126) | 2.4574 (2.4019–2.5021) | -0.8% | True |
| import/import-narrow | file | 1.0854 (1.0564–1.1031) | 1.0669 (1.0044–1.0782) | +1.7% | True |
| import/import-narrow | stream | 1.2166 (1.0263–1.2670) | 1.2620 (1.0777–1.3765) | -3.6% | True |
| import/import-wide | file | 2.8108 (2.7906–2.9146) | 2.8066 (2.7743–2.8425) | +0.1% | True |
| import/import-wide | stream | 2.8309 (2.7825–2.8724) | 2.8241 (2.7772–2.8751) | +0.2% | True |

## Verified-4× query throughput

Thirty trials: three five-second request windows plus drain per query/concurrency case. 100K × 10 seed-42 sales fixture. Complete results checked against independently calculated Python/Arrow references before each timed case; every timed request fully fetches Arrow and checks row count. Clones share the automatic worker budget. QPF stays at 4×; this run makes no Standard/2×/8× comparison or QPF speedup claim.

| Query | Clients | QPS: median (range) | Median p95 ms |
|---|---|---|---|
| driver_group | 1 | 1.94 (1.93–1.95) | 546.5 |
| driver_group | 4 | 5.22 (5.04–5.26) | 860.9 |
| driver_reduce | 1 | 5.93 (5.87–5.95) | 193.0 |
| driver_reduce | 4 | 7.88 (6.81–8.03) | 649.9 |
| full_scan | 1 | 1.27 (1.25–1.28) | 821.9 |
| full_scan | 4 | 3.38 (3.34–3.50) | 1335.4 |
| index_count | 1 | 78.59 (75.98–78.77) | 14.1 |
| index_count | 16 | 435.46 (430.60–437.90) | 44.9 |
| index_group | 1 | 4.20 (4.17–4.20) | 258.6 |
| index_group | 16 | 11.91 (11.85–12.32) | 2095.6 |

The first throughput attempt was excluded after both fixture TLS endpoints reset connections. The task-owned server was restarted and effective 4x QPF/Search/TLS readiness reverified; the final three rounds were rerun from the beginning.

## Client worker policies

Twelve fresh policy trials produce 36 concurrency records; all queries validate independently computed SUM/AVG/MIN/MAX over 32,768 rows with aggregate pushdown disabled. Four policies, three shuffled repetitions, 1/2/4 concurrent statements. First cold wave, ten learning waves, ten measured waves. macOS client with GOMAXPROCS=2, without an OS CPU/RAM cap. This differs from the old Linux two-CPU/two-GiB, 60-learning-wave experiment: the old 10/12 gate claim is not reused.

| Policy | Statements | QPS: median (range) | Median p95 ms |
|---|---|---|---|
| adaptive | 1 | 13.14 (13.10–13.46) | 85.0 |
| adaptive | 2 | 13.67 (12.83–14.01) | 157.3 |
| adaptive | 4 | 14.13 (13.67–14.37) | 314.6 |
| fixed2 | 1 | 13.41 (12.94–13.69) | 82.6 |
| fixed2 | 2 | 14.20 (12.89–14.31) | 150.5 |
| fixed2 | 4 | 12.48 (12.38–12.63) | 329.6 |
| fixed4 | 1 | 14.80 (14.80–15.01) | 72.8 |
| fixed4 | 2 | 16.44 (15.89–16.66) | 132.3 |
| fixed4 | 4 | 17.72 (16.98–18.11) | 235.8 |
| serial | 1 | 7.95 (7.82–8.17) | 137.1 |
| serial | 2 | 14.37 (14.34–14.40) | 144.6 |
| serial | 4 | 14.57 (14.01–14.97) | 299.1 |

## Matched wide transfer

All 72 complete outputs validate exact values/multiplicity: one warmup and five measured repetitions per engine/lifecycle/reader count. Same typed 100K × 105 source and Arrow IPC output. Redis uses public partitions for four readers; PostgreSQL/ClickHouse use disjoint ordinal ranges. Persistent readers reuse connections; spawned readers include process/import/connect/close. Preparation/loading and verification excluded.

PostgreSQL 16.10/ADBC COPY and ClickHouse 25.8/ArrowStream; each server has four CPUs/eight GiB. Redis uses TLS; reference localhost protocols are plaintext. PostgreSQL fsync/synchronous_commit/full_page_writes are off; ClickHouse IPC is uncompressed. This is a scoped resident-data transfer comparison, not equal-cost, durability or product-wide evidence.

| Engine | Lifecycle | Readers | Seconds: median (range) |
|---|---|---|---|
| clickhouse | persistent | 1 | 0.174 (0.163–0.225) |
| clickhouse | persistent | 4 | 0.142 (0.140–0.148) |
| clickhouse | spawned | 1 | 0.369 (0.356–0.405) |
| clickhouse | spawned | 4 | 0.369 (0.345–0.383) |
| postgres | persistent | 1 | 0.368 (0.360–0.378) |
| postgres | persistent | 4 | 0.204 (0.185–0.213) |
| postgres | spawned | 1 | 0.619 (0.607–0.672) |
| postgres | spawned | 4 | 0.493 (0.472–0.518) |
| redis | persistent | 1 | 3.155 (3.114–3.190) |
| redis | persistent | 4 | 1.255 (1.244–1.512) |
| redis | spawned | 1 | 3.329 (3.281–3.455) |
| redis | spawned | 4 | 1.582 (1.533–1.664) |

## Publication and verification

`python3 audit.py` recomputes summary arithmetic and validates complete case/repetition coverage from sanitized measurements. It does not rerun SQL or independently recover discarded output rows. Original runs completed the full validators described above. Raw operational artifacts and edit payloads remain private. No production driver code changed.

The future 99,997,497-row harness default selects Flex stopgap (`adbc.redis.flex=true`), explicitly propagated to statement and partition connections. It does not provision Flex or override v0.0.25’s managed-snapshot refusal. The user canceled that rerun. Unchanged Flight comparison measurements retain their October 30 v0.0.18 label.

Harness checks: 124 tests ran, 121 passed and three opt-in Linux/systemd isolation tests skipped (`CLICKBENCH_SYSTEMD_TEST` unset on macOS). The focused `TestSnapshotFlexExecutionRefusesBeforeDial` unit test passed. Neither result is a new live Flex benchmark. Native slide content and layout verification are recorded in the change validation document.
