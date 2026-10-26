# Opt-in balanced partitions: local evidence, 2026-10-21

Issue #379; follows readiness fixes #381/#383. This reduced-data local pilot
uses the official ClickBench 100,000-row prefix and all 105 columns, retaining
the final 10,000 rows. The two workloads are supplemental SQL, not official
leaderboard queries. All 30 final trials passed exact source/DuckDB values and
planned/ordinary Arrow schema checks.

| Workload | Method | Max workers | Median full-fetch seconds | Median planning seconds |
|---|---|---:|---:|---:|
| scan_narrow | ExecuteQuery | 1 | 0.4402 | 0.0000 |
| scan_narrow | rowid / 8 descriptors | 1 | 1.4850 | 0.0257 |
| scan_narrow | balanced / 8 descriptors | 1 | 1.6979 | 0.1036 |
| scan_narrow | rowid / 8 descriptors | 4 | 0.7520 | 0.0341 |
| scan_narrow | balanced / 8 descriptors | 4 | 0.7394 | 0.1084 |
| scan_wide | ExecuteQuery | 1 | 0.8191 | 0.0000 |
| scan_wide | rowid / 8 descriptors | 1 | 1.8481 | 0.0307 |
| scan_wide | balanced / 8 descriptors | 1 | 1.7121 | 0.0606 |
| scan_wide | rowid / 8 descriptors | 4 | 1.2639 | 0.0300 |
| scan_wide | balanced / 8 descriptors | 4 | 0.8754 | 0.0643 |

The default splits produced descriptor row counts 0,0,0,0,0,0,0,10000.
Balanced splits produced 1250 rows in each descriptor. With four workers, wide
reads improved from 1.2639s to 0.8754s (about 1.44x versus the current partition
strategy), while ExecuteQuery remained faster at 0.8191s. Narrow reads were
nearly unchanged. One-worker narrow reads were slower with balancing. This
does not justify enabling balancing by default or a general speedup claim.

Timing includes planning, spawned process/connection startup, complete fetch
and Arrow IPC writes. Complete value/schema validation is outside timing.
Three repetitions with rotated method order; no cold-cache or exclusive-host
claim. A dedicated Redis container ran on localhost:17479; unrelated local
debug containers were also running. Platform/package/server versions, source
revisions, binary/source hashes, readiness state, worker RSS and sampled
client/server memory/network metrics and their limitations are in comparison.json.

The initial correct binary-search planner is preserved separately in
initial-binary-probes.json: planning overhead erased its benefit at this scale.
The final planner reuses prefix probes and interpolates count targets. These
are different local runs with different host conditions; no causal before/after
performance ratio is claimed between those files. Two intervening attempts were
interrupted by external SIGTERM shutdowns of the old test container (clean exit,
not an OOM/crash); incomplete attempts are excluded from summary ratios.

Reproduction: use benchmarks/clickbench/partition_profile.py with the same
official source prefix and native library; REDIS_URI must name a disposable
Search server. Pass --rows 100000 --layout skewed --repeats 3 --partitions 8
--workers 1 4 --strategies rowid balanced --workloads scan_narrow scan_wide
and a fresh --output. Data/schema and upstream SQL provenance are in the JSON.
The official source Parquet is not vendored. No paid resources were provisioned.
