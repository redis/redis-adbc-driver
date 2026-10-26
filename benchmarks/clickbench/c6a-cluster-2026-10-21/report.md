# ClickBench: six c6a.4xlarge primaries

2026-10-21. **Pilot completed: 100,000 rows, all 105 columns, 43 queries,
three trials each; all 129 outputs independently validated.** Full scale
(99,997,497 rows) was not attempted on this six-node allocation because the measured pilot memory footprint
projects far beyond this cluster's RAM. These are reduced-data measurements,
not a full-scale leaderboard entry or a comparison score.

The subsequent [48-primary full-scale run](../full-c6a-cluster-2026-10-21/README.md)
is recorded separately; its measurements do not change this pilot's scope.

## Allocation and configuration

| Property | Observed/configured value |
| --- | --- |
| AWS region / stable AZ | us-east-1 / use1-az6 |
| Instances | Six c6a.4xlarge, each 16 vCPUs and 32 GiB RAM |
| Aggregate host resources | 96 vCPUs, 192 GiB RAM |
| Redis topology | Six OSS Cluster primaries, zero replicas; all 16,384 slots covered |
| ADBC placement | On the first primary's instance; no extra client machine |
| Redis version / Search | 8.6.2 / 8.6.0 |
| Image | redis:8.6.2, digest 832d7785830f3f4b559300e6191fc914b15642c1935252338825cf4332200148 |
| Per-node Redis limits | 24 GiB maxmemory, no eviction; 27 GiB container memory, no swap |
| Query boundary | Separate systemd service per trial, 4 GiB RAM, 60-second deadline |
| Network / durability | Private TCP; no TLS, persistence, or replicas |
| Storage | Encrypted gp3 root volumes, deleted on instance termination |

Matching the instance type matches the hardware **per node**. Six nodes provide
six times the aggregate CPU and RAM of a single c6a.4xlarge published baseline.
The client also competes with one primary for host resources.

## Measurements and scale decision

Loading took **5.80 seconds**, including typed source decoding,
DDL, ingestion, index readiness and count verification, excluding source download.
The median across queries of the best second/third timing was
**345.64 ms**. This is descriptive of this pilot;
it is not the ClickBench shifted geometric-mean score. The first trial is not
a true cold-cache measurement.

After the run, the six Redis processes reported **939,249,056
bytes (0.875 GiB) used memory** and
1.050 GiB RSS in total. This measures whole dedicated
Redis processes, including overhead, rather than claiming an exact table size.
Global FT.INFO replies are coordinated and must not be added together; direct
per-node INFO memory values are summed instead.

A simple linear projection to 99,997,497 rows is **about
875 GiB** of Redis used memory,
versus 192 GiB host RAM and 144 GiB configured Redis maxmemory across the six
nodes. **This projection is an estimate, not a full-data measurement or an
observed full-scale OOM.** Data distribution, cardinality and allocation overhead
may change with scale. Its size relative to the allocation was sufficient to
avoid an expensive full-data attempt on this six-node configuration. Increasing
only disk capacity cannot extend this RAM-only Redis data budget.

## Correctness and reproducibility

The standard validator recorded 126 valid trials and three unresolved Q19 ties.
[Supplemental Q19 evidence](evidence/results/pilot-100k-v3-q19-correctness.json)
validates all three against independent DuckDB groups, complete output values,
multiplicity and rank bands. Only the oracle receives an alias for COUNT(*) so
the tie validator can name its sort column; timed SQL is unchanged. The standard
validator's unresolved records are preserved. Its comparison tool will continue
to exclude those Q19 timings unless supplemental evidence is integrated there.

- Driver source: `9f5188c411f1e3a3bf1423a72089e2441b7995bd`; Go 1.26.6, native Linux amd64 build.
- Driver binary SHA256: `0ee09756ff90cc51ecc46be0c2106de6bbd62456630618a1f8c03a28bc23225e`.
- Upstream ClickBench: `ccda3033f3c7303d9d7f9634d6dae118ef085f96`, official PostgreSQL SQL.
- Source: first 100,000 rows of the official hits_compatible Parquet; SHA256
  `d053397f7aa5b2e1ee9160779a9e9307b162c6994aebba829c7ea1411076c4d0`. [Source record](source.json).
- [Run metadata and timings](evidence/results/pilot-100k-v3.json),
  [standard correctness](evidence/results/pilot-100k-v3-correctness.json),
  [memory evidence](evidence/results/memory-final.json),
  [infrastructure](infrastructure.json), [dependencies](evidence/dependencies.txt).
- [Raw archive](raw-artifacts.tar.gz) contains every trial output, job and
  diagnostic checkpoint, earlier setup-attempt records, and verification logs.
  The two earlier attempts were excluded: one failed ingestion before loading;
  the other was stopped before timing while correcting readiness checks.

Harness changes add explicit self-hosted mode, preserve Cloud defaults and full
AWS FinOps checks, check every coordinator's global indexed count, and map Arrow
ingestion fields to the lowercase names created by unquoted PostgreSQL DDL.
[Q19 supplemental validator](validate_q19.py) is retained with the evidence.
Verification: 67 helper/unit checks passed (70 discovered, three opt-in skips),
then all three real Linux cgroup credential/OOM/timeout checks passed.

## Per-query timing

All times are milliseconds and include execute plus complete Arrow fetch.
“Hot” is the smaller of trials two and three. No baseline ratios are calculated
because published full-data timings are not comparable to this pilot.

| Query | Trial 1 (ms) | Hot (ms) | Correctness |
| --- | ---: | ---: | --- |
| 1 | 5.64 | 6.59 | DuckDB valid |
| 2 | 208.37 | 205.25 | DuckDB valid |
| 3 | 15.75 | 15.08 | DuckDB valid |
| 4 | 226.42 | 226.05 | DuckDB valid |
| 5 | 478.72 | 472.93 | DuckDB valid |
| 6 | 52.74 | 46.12 | DuckDB valid |
| 7 | 10.11 | 10.56 | DuckDB valid |
| 8 | 12.10 | 12.83 | DuckDB valid |
| 9 | 529.08 | 522.15 | DuckDB valid |
| 10 | 657.77 | 628.71 | DuckDB valid |
| 11 | 506.65 | 499.94 | DuckDB valid |
| 12 | 545.33 | 543.08 | DuckDB valid |
| 13 | 51.40 | 47.81 | DuckDB valid |
| 14 | 563.94 | 563.63 | DuckDB valid |
| 15 | 76.49 | 70.78 | DuckDB valid |
| 16 | 469.67 | 460.14 | DuckDB valid |
| 17 | 582.80 | 552.49 | DuckDB valid |
| 18 | 555.62 | 555.18 | DuckDB valid |
| 19 | 946.77 | 903.57 | Supplemental Q19 tie check |
| 20 | 6.12 | 6.06 | DuckDB valid |
| 21 | 225.51 | 220.27 | DuckDB valid |
| 22 | 559.82 | 555.05 | DuckDB valid |
| 23 | 1399.39 | 1380.82 | DuckDB valid |
| 24 | 1001.08 | 993.80 | DuckDB valid |
| 25 | 316.54 | 318.83 | DuckDB valid |
| 26 | 747.93 | 740.52 | DuckDB valid |
| 27 | 351.14 | 345.64 | DuckDB valid |
| 28 | 650.00 | 643.97 | DuckDB valid |
| 29 | 783.03 | 787.44 | DuckDB valid |
| 30 | 290.44 | 280.10 | DuckDB valid |
| 31 | 685.58 | 664.39 | DuckDB valid |
| 32 | 867.70 | 875.00 | DuckDB valid |
| 33 | 1206.16 | 1187.98 | DuckDB valid |
| 34 | 213.97 | 201.91 | DuckDB valid |
| 35 | 574.44 | 564.23 | DuckDB valid |
| 36 | 616.51 | 616.51 | DuckDB valid |
| 37 | 6.65 | 6.18 | DuckDB valid |
| 38 | 7.04 | 6.93 | DuckDB valid |
| 39 | 6.55 | 6.81 | DuckDB valid |
| 40 | 5.73 | 5.56 | DuckDB valid |
| 41 | 7.23 | 6.56 | DuckDB valid |
| 42 | 7.43 | 6.97 | DuckDB valid |
| 43 | 5.64 | 4.78 | DuckDB valid |

## Cleanup

All six temporary instances are verified **terminated**. The security group
and AWS key pair are verified absent; the generated local private key was
removed. Two root volumes are verified absent. Four remaining root volumes are
still in AWS `deleting` state; complete disk deletion is not yet verified.
[Cleanup evidence](cleanup.json) records the latest readback. No running
benchmark compute remains.
