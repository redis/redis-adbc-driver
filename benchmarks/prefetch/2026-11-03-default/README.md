# Default prefetch qualification, November 3–4, 2026

Decision: enable bounded prefetch by default for eligible multi-page wide managed HASH streams. Keep known single-page, narrow, GPU and unsupported plans on their existing paths. The build-only `scanprefetchoff` tag retains a synchronous baseline; legacy `scanprefetch` remains accepted.

## Method and source identity

Primary matched runtime: `158b8c820c476a7ae116fb677c6f1fffa4722b98`, with prefetch compiled off/on. This includes the reproduced consumer-EOF readiness fix and known-single-page guard, after PR #297. Both trees are identical, built with Go 1.27.3 Linux amd64 and the same linker flags. [Build identities](build-identities.json) retain source-tree/binary hashes and dependency versions. The final normal-default candidate is `07bf8decabfed75c9f1a93d27051f16970e0c30c`; a separate matched Go/C confirmation validates that build and its opt-out. [Default build identities](default-build-identities.json) verify existing production source files match the primary code except the two selectors, and the newly merged isolated SIMD package is not linked into the driver.

Each of four primary matrices has eight cases: wide/three-column control × regular/public partitions × one/four readers. Each build/case has one excluded warm-up and twenty alternating-order measured trials: **336 executions per matrix, 1,344 total**. All cases fetch exactly 100,000 rows and independently validate every selected value, physical Arrow type, NULL bit and duplicate multiplicity outside timing. Every requested reader was active. The full 105-column and eligibility-threshold 64-column matrices are separate. GOMAXPROCS 4 is a Go-runtime limit on the same 16-vCPU host, not a separate four-vCPU VM.

Redis Software container 8.2.0-78.18, Redis 8.6.2, Search 8.6.11; data connections use authenticated verified TLS and RESP3. Effective **4x QPF**, six Search workers, mnp and 32 connections are verified before every execution. Same-AZ 48 server hosts plus one client, each c6a.4xlarge (16 vCPU, 32 GiB); server container limit 29 GiB, database memory 900 GiB, persistence disabled, no eviction. This is a large controlled topology relative to the queried fixture, not a production sizing or universal workload claim.

All 48 snapshot SHA-256 values were checked, and **99,997,497 rows** restored before fixture preparation. Historical snapshot provenance is Community; the restore and all these tests run exclusively on Software. The complete restored table is preserved during testing. A small 100K managed fixture is copied with pinned official DDL and SQL INSERT SELECT of exact restored IDs outside timing; no Parquet ingestion or full-scale reload. Native exact-ID/unique-WatchID membership is joined to the checksum-verified official Parquet, and every expected typed value comes from that source. [Source proof](source-proof.json) and [fixture preparation](fixture-preparation.json) describe this; the physical independent Arrow reference and raw IPC results are archived privately with checksums. Parallel loading does not imply a Parquet-prefix order.

Connections are preopened. Timed work includes public execute/partition planning, complete Arrow fetch, IPC output and stream/statement/cursor cleanup; connection/database teardown and independent validation are excluded. Four public partition descriptors are consumed by one/four readers. Four ordinary readers use disjoint exact ID ranges. Go uses its Arrow allocator; C/Python uses the public shared-library boundary and unmodified PyArrow batches. C Go-allocation/heap counters are unavailable, not zero. Do not combine allocator metrics across boundaries.

## Elapsed results

Seconds are median [minimum, maximum], excluding warm-ups. Change compares build medians; the bracketed interval separately bootstraps the median of paired percentage changes (10,000 seeded resamples). It describes this run/fixture, not a universal confidence guarantee. Wins count faster candidate pairs out of twenty. The predeclared gate requires repeatable one-reader wide benefit and wide upper paired bounds within a +5% concurrent-regression margin. Narrow controls remain visible.

### Go, 105 columns, GOMAXPROCS 16

| Projection/API/readers | Baseline seconds | Prefetch seconds | Median change | Paired interval | Wins |
|---|---:|---:|---:|---:|---:|
| narrow/partition/1 | 1.966 [1.918, 2.012] | 1.965 [1.938, 2.016] | -0.05% | [-0.91%, +0.76%] | 10/20 |
| narrow/partition/4 | 0.801 [0.734, 0.867] | 0.804 [0.742, 0.876] | +0.41% | [-2.01%, +2.30%] | 12/20 |
| narrow/regular/1 | 2.402 [2.351, 2.479] | 2.399 [2.357, 2.450] | -0.15% | [-1.25%, +0.63%] | 10/20 |
| narrow/regular/4 | 1.244 [1.181, 1.351] | 1.237 [1.140, 1.351] | -0.62% | [-4.29%, +3.85%] | 11/20 |
| wide/partition/1 | 8.246 [8.142, 8.327] | 7.013 [6.896, 7.095] | -14.96% | [-15.53%, -14.41%] | 20/20 |
| wide/partition/4 | 3.008 [2.727, 3.199] | 2.811 [2.637, 2.924] | -6.57% | [-10.14%, -3.25%] | 19/20 |
| wide/regular/1 | 8.982 [8.858, 9.149] | 7.715 [7.537, 7.846] | -14.10% | [-15.11%, -13.39%] | 20/20 |
| wide/regular/4 | 3.632 [3.375, 4.135] | 3.592 [3.310, 3.827] | -1.10% | [-5.91%, +2.06%] | 13/20 |

### Go, 64 columns, GOMAXPROCS 16

| Projection/API/readers | Baseline seconds | Prefetch seconds | Median change | Paired interval | Wins |
|---|---:|---:|---:|---:|---:|
| narrow/partition/1 | 1.967 [1.923, 2.014] | 1.976 [1.917, 2.017] | +0.43% | [-0.89%, +1.31%] | 7/20 |
| narrow/partition/4 | 0.811 [0.749, 0.896] | 0.791 [0.755, 0.834] | -2.51% | [-4.76%, +0.06%] | 14/20 |
| narrow/regular/1 | 2.398 [2.334, 2.459] | 2.384 [2.333, 2.428] | -0.60% | [-1.37%, +0.13%] | 13/20 |
| narrow/regular/4 | 1.230 [1.161, 1.365] | 1.239 [1.171, 1.316] | +0.77% | [-4.38%, +4.35%] | 10/20 |
| wide/partition/1 | 5.797 [5.746, 5.873] | 5.037 [4.960, 5.139] | -13.10% | [-13.65%, -12.80%] | 20/20 |
| wide/partition/4 | 2.042 [1.914, 2.421] | 1.979 [1.832, 2.133] | -3.10% | [-6.75%, -1.46%] | 16/20 |
| wide/regular/1 | 6.543 [6.442, 6.662] | 5.740 [5.675, 5.812] | -12.28% | [-12.89%, -12.11%] | 20/20 |
| wide/regular/4 | 2.851 [2.586, 3.098] | 2.674 [2.525, 2.875] | -6.20% | [-8.92%, +0.16%] | 14/20 |

### C/Python, 105 columns

| Projection/API/readers | Baseline seconds | Prefetch seconds | Median change | Paired interval | Wins |
|---|---:|---:|---:|---:|---:|
| narrow/partition/1 | 1.996 [1.961, 2.036] | 1.994 [1.969, 2.031] | -0.11% | [-0.60%, +0.78%] | 9/20 |
| narrow/partition/4 | 0.800 [0.755, 0.900] | 0.802 [0.742, 0.873] | +0.27% | [-2.06%, +2.80%] | 10/20 |
| narrow/regular/1 | 2.412 [2.377, 2.455] | 2.402 [2.358, 2.429] | -0.44% | [-1.01%, +0.12%] | 13/20 |
| narrow/regular/4 | 1.265 [1.189, 1.367] | 1.235 [1.175, 1.369] | -2.38% | [-4.62%, +0.38%] | 13/20 |
| wide/partition/1 | 8.294 [8.206, 8.434] | 7.104 [7.009, 7.193] | -14.35% | [-14.78%, -13.93%] | 20/20 |
| wide/partition/4 | 3.034 [2.732, 3.447] | 2.770 [2.639, 3.018] | -8.70% | [-7.79%, -4.78%] | 18/20 |
| wide/regular/1 | 9.064 [8.958, 9.295] | 7.792 [7.558, 7.918] | -14.03% | [-14.90%, -13.36%] | 20/20 |
| wide/regular/4 | 3.709 [3.500, 4.075] | 3.505 [3.291, 3.736] | -5.52% | [-6.66%, -1.85%] | 18/20 |

### Go, 105 columns, GOMAXPROCS 4

| Projection/API/readers | Baseline seconds | Prefetch seconds | Median change | Paired interval | Wins |
|---|---:|---:|---:|---:|---:|
| narrow/partition/1 | 1.977 [1.950, 2.008] | 1.970 [1.937, 2.048] | -0.34% | [-1.90%, +0.50%] | 10/20 |
| narrow/partition/4 | 0.798 [0.752, 0.874] | 0.800 [0.722, 0.845] | +0.32% | [-4.76%, +2.71%] | 11/20 |
| narrow/regular/1 | 2.409 [2.348, 2.532] | 2.401 [2.372, 2.472] | -0.30% | [-1.52%, +0.96%] | 11/20 |
| narrow/regular/4 | 1.276 [1.174, 1.335] | 1.228 [1.160, 1.310] | -3.77% | [-4.55%, +0.14%] | 14/20 |
| wide/partition/1 | 8.274 [8.172, 8.441] | 7.034 [6.923, 7.163] | -14.99% | [-15.67%, -14.47%] | 20/20 |
| wide/partition/4 | 2.894 [2.669, 3.123] | 2.789 [2.648, 3.198] | -3.62% | [-8.04%, +1.07%] | 14/20 |
| wide/regular/1 | 8.998 [8.862, 9.077] | 7.734 [7.509, 7.884] | -14.04% | [-14.92%, -12.90%] | 20/20 |
| wide/regular/4 | 3.845 [3.464, 4.143] | 3.517 [3.434, 3.787] | -8.53% | [-8.92%, -3.11%] | 20/20 |

All four wide elapsed gates passed. Some concurrent cases are near-neutral and their paired intervals include zero; do not describe every lower median as a proven gain. Promotion is based on repeated complete-fetch elapsed behavior and correctness, not allocation reductions.

## Client costs, memory and batches

Below are wide-case medians, baseline → candidate. CPU is process CPU during the timed interval. Allocation GB are cumulative Go bytes, not live heap. RSS is independently sampled current child PID RSS every 20 ms across its process lifetime, excluding the Python parent/reference, and is not a strict peak. Go live heap is separately sampled; full medians/ranges and all narrow controls are in each machine-readable assessment. The older child ru_maxrss field can inherit fork high-water and is retained only as a diagnostic.

| Lane/API/readers | CPU seconds | Allocated GB | Sampled Go heap MB | Sampled child RSS MB | First batch rows |
|---|---:|---:|---:|---:|---:|
| Go, 105 columns, GOMAXPROCS 16/partition/1 | 4.737 → 4.754 | 1.174 → 1.175 | 99.400 → 97.986 | 147.786 → 147.206 | [25000] → [25000] |
| Go, 105 columns, GOMAXPROCS 16/partition/4 | 4.708 → 4.719 | 1.174 → 1.175 | 292.221 → 294.609 | 354.947 → 357.323 | [25000] → [25000] |
| Go, 105 columns, GOMAXPROCS 16/regular/1 | 4.399 → 4.354 | 1.162 → 1.162 | 162.276 → 158.817 | 217.606 → 212.785 | [65536] → [65536] |
| Go, 105 columns, GOMAXPROCS 16/regular/4 | 4.776 → 4.857 | 1.178 → 1.179 | 283.018 → 289.085 | 348.328 → 349.960 | [25000] → [25000] |
| Go, 64 columns, GOMAXPROCS 16/partition/1 | 3.256 → 3.226 | 0.866 → 0.867 | 73.752 → 74.039 | 122.184 → 122.528 | [25000] → [25000] |
| Go, 64 columns, GOMAXPROCS 16/partition/4 | 3.140 → 3.179 | 0.866 → 0.867 | 218.474 → 227.928 | 278.020 → 280.275 | [25000] → [25000] |
| Go, 64 columns, GOMAXPROCS 16/regular/1 | 2.976 → 2.946 | 0.856 → 0.856 | 124.671 → 124.510 | 175.172 → 175.399 | [65536] → [65536] |
| Go, 64 columns, GOMAXPROCS 16/regular/4 | 3.190 → 3.229 | 0.870 → 0.871 | 210.355 → 206.790 | 268.857 → 272.294 | [25000] → [25000] |
| C/Python, 105 columns/partition/1 | 5.270 → 5.127 | unavailable → unavailable | unavailable → unavailable | 238.451 → 256.162 | [25000] → [25000] |
| C/Python, 105 columns/partition/4 | 5.361 → 5.459 | unavailable → unavailable | unavailable → unavailable | 363.057 → 383.308 | [25000] → [25000] |
| C/Python, 105 columns/regular/1 | 5.167 → 4.983 | unavailable → unavailable | unavailable → unavailable | 273.637 → 290.789 | [65536] → [65536] |
| C/Python, 105 columns/regular/4 | 5.422 → 5.393 | unavailable → unavailable | unavailable → unavailable | 348.068 → 371.608 | [25000] → [25000] |
| Go, 105 columns, GOMAXPROCS 4/partition/1 | 4.290 → 4.205 | 1.174 → 1.175 | 107.511 → 103.581 | 155.738 → 150.213 | [25000] → [25000] |
| Go, 105 columns, GOMAXPROCS 4/partition/4 | 4.098 → 4.142 | 1.173 → 1.174 | 296.635 → 318.571 | 356.678 → 373.752 | [25000] → [25000] |
| Go, 105 columns, GOMAXPROCS 4/regular/1 | 4.054 → 3.971 | 1.161 → 1.162 | 149.794 → 161.588 | 203.866 → 210.213 | [65536] → [65536] |
| Go, 105 columns, GOMAXPROCS 4/regular/4 | 4.203 → 4.249 | 1.178 → 1.178 | 289.713 → 297.422 | 350.239 → 356.639 | [25000] → [25000] |

The deterministic ownership bound is the current reply page plus at most one additional owned page per reader, following the existing 4,096-row discovery-page request. It is not a byte quota. A controlled synthetic retained-heap diagnostic with 4,096 × 105 unique roughly 32-byte strings used about 28.44 MB for one additional page. Four readers have four page budgets; variable-width values change actual costs. Arrow batch size is a separate setting.

C wide process-RSS medians are approximately 17–24 MB higher in the candidate. C narrow controls also have an RSS offset even though the narrow path does not start prefetch; the live path assertion passes. Therefore the entire process-RSS delta must not be presented as a direct page-retention measurement. Go sampled RSS medians are close between builds. Neither sampled RSS nor GC heap samples establish a strict worst-case bound.

## Wire diagnostic

A separate 32-execution diagnostic has one warm-up and one measured run per build/case. Every fetch is independently validated. A TLS-transparent relay counts actual request/reply TCP payload, including TLS setup/framing and excluding IP headers and separate INFO control traffic. Proxy elapsed is excluded from all primary tables and from the gate. Software endpoint INFO CPU deltas are not a per-shard decomposition and do not establish server CPU savings.

| Projection/API/readers | Request MB baseline → candidate | Reply MB baseline → candidate |
|---|---:|---:|
| narrow/partition/1 | 8.729 → 8.729 | 22.245 → 22.245 |
| narrow/partition/4 | 8.737 → 8.737 | 22.256 → 22.257 |
| narrow/regular/1 | 8.714 → 8.714 | 24.093 → 24.092 |
| narrow/regular/4 | 8.739 → 8.739 | 25.063 → 25.063 |
| wide/partition/1 | 193.480 → 193.480 | 151.854 → 151.977 |
| wide/partition/4 | 193.488 → 193.488 | 151.865 → 151.988 |
| wide/regular/1 | 193.464 → 193.464 | 153.686 → 153.746 |
| wide/regular/4 | 193.490 → 193.490 | 154.705 → 154.854 |

The server reply format is unchanged. Prefetch overlaps the existing work; it does not implement typed server export or remove RESP framing.

## Final normal-build confirmation and correctness

After the declared primary gates passed, the normal-build candidate and its opt-out were checked on the restored fixture. Both Go and C/Python then ran one warm-up plus five alternating-order measured pairs per case, adding **192 independently validated executions**. The total qualification contains **1,568 executions**, including the separate wire diagnostic. The final reports and selection/build identities are retained separately; their timings are not pooled with the twenty-pair matrices.

Final five-pair wide confirmation (kept separate from primary matrices):

| Boundary/API/readers | Median change | Paired interval |
|---|---:|---:|
| Go/partition/1 | -14.54% | [-14.90%, -13.97%] |
| Go/partition/4 | -4.10% | [-8.71%, -0.90%] |
| Go/regular/1 | -14.03% | [-15.52%, -12.82%] |
| Go/regular/4 | -8.28% | [-16.99%, +0.51%] |
| C/Python/partition/1 | -14.32% | [-15.71%, -13.21%] |
| C/Python/partition/4 | -8.31% | [-9.60%, -2.14%] |
| C/Python/regular/1 | -13.12% | [-14.75%, -12.77%] |
| C/Python/regular/4 | -6.01% | [-16.22%, +4.10%] |

See the [Go report](default-go-results.json), [C report](default-c-results.json), [on/off confirmation proof](default-confirmation-verification.json) and [normal-build selection](default-selection-on.json).

The full default-enabled Software race suite passed with normal build flags: driver 356.591 s, 840 top-level passes and 43 existing capability/opt-in skips, with six tested packages passing and a CUDA stub package without tests. The corrected experimental build also passed the full suite before timing (417.459 s, 837 passes/41 skips before the isolated SIMD integration). The explicit opt-out unit suite and Python validator/meter/RSS/report tests pass. See the [validation record](../../../docs/sdlc/changes/prefetch-default/validation.md) and linked JSON for exact commands and final C checks.

Two failures were deliberately reproduced before fixing them: a table drop after producer EOF could incorrectly become successful completion, and a known single-page discovery unnecessarily started a worker. Consumer EOF now rechecks readiness after conversion; known single-page discoveries remain synchronous. Both regression tests pass under the race detector. Raw failure logs are retained privately and [sanitized failures](regressions.json) identify the observations. No assertion or skip was weakened. Local monitor dependency loss was repaired in a task-owned environment; remote trials were unaffected.

No human maintainer approval is claimed. The normal-default source was separately confirmed; historical #297 numbers are not relabeled as this run. No native SIMD SQL integration or native typed-export capability is introduced.

## Evidence and cleanup

The adjacent manifest checksums every published result. Full trial JSON retains warm-ups, complete values-validation flags, active-reader checks, actual first-batch sizes/latencies, CPU/allocations/heap/RSS and per-trial QPF proof. Raw IPC, physical independent source, failures and operational records remain in a private checksummed archive; endpoints, resource IDs, credentials and license files are not published.

[Cleanup verification](cleanup.json) records the exact task-owned teardown and conservative compute estimate, including the prior run for the original aggregate $300 cap. Any remaining AWS asynchronous disk-deletion state is recorded explicitly rather than claimed physically absent. Existing backups and shared IAM resources are retained.
