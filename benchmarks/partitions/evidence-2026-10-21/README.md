# Partition API comparison: local evidence, 2026-10-21

Follow-up to [#170](https://github.com/redis/redis-adbc-driver/issues/170),
stacked on [refactor #376](https://github.com/alberttwong/redis-adbc-driver/pull/376).
This is a reduced-data local pilot, not a full-scale or Cloud ClickBench ranking.

All **330 readiness-gated trials** completed exact value and Arrow schema checks.
Only official **Q20** is partition-supported; the other 42 queries are explicitly
NOT_IMPLEMENTED. Q20 returns zero rows on this prefix and cannot establish scan throughput.

## Readiness finding and unresolved runtime work

The first harness checked SQL counts without waiting for Search indexing. Two
sparse attempts returned 295 instead of 294 filtered rows, repeating final ID
100000. Failing JSON and complete actual/expected Arrow output are retained in
[diagnostics](diagnostics/). Later ungated 75/60/300-trial runs passed; those
passes do not erase the failures. The 300-trial diagnostic used expanded slowlog
settings and concurrent raw probes, so its timings are not benchmark results.

A raw redis-py reproducer, without Go/ADBC/Arrow, returned 10011 and 10003
rows for 10000 physical rows immediately after sparse deletes. FT.INFO recorded
num_docs=10000 but indexing=1. The failures occurred in RESP2; RESP3 ran later
on the same index and is not an equivalent test of that initial condition.
A control waiting for num_docs=10000 and indexing=0 passed all 400 raw reads.
This supports an index-building condition; the precise mechanism and causal
connection to every ADBC failure remain unproven. It does not establish a
complete runtime mitigation. [#175](https://github.com/redis/redis-adbc-driver/issues/175)
tracks safe handling while indexing and further deployment verification.
The benchmark now waits for readiness after ingestion and layout changes.
The driver itself has not gained a readiness guard or snapshot guarantee.

## Repeated read results

Median seconds over three repetitions. Partition column uses eight descriptors
and at most four spawned workers. Ratio >1 means partitions were faster.
Each read includes fresh worker/connection startup, complete fetch and IPC output;
parent result validation is outside timing. Method order rotates. No cold-cache claim.

| Layout / allocated IDs | Workload | Rows returned | ExecuteQuery | Partitions | Speed ratio |
|---|---|---:|---:|---:|---:|
| dense / 100000 | scan_narrow | 100000 | 0.8077 | 0.4970 | 1.63x |
| dense / 100000 | scan_wide | 100000 | 5.3932 | 3.2067 | 1.68x |
| dense / 100000 | filter_adv | 2885 | 0.8107 | 0.5176 | 1.57x |
| dense / 100000 | expression_region | 45950 | 0.4575 | 0.3595 | 1.27x |
| sparse / 100000 | scan_narrow | 10000 | 0.2506 | 0.2669 | 0.94x |
| sparse / 100000 | scan_wide | 10000 | 0.7473 | 0.5186 | 1.44x |
| sparse / 100000 | filter_adv | 294 | 0.2595 | 0.2615 | 0.99x |
| sparse / 100000 | expression_region | 4583 | 0.2239 | 0.2423 | 0.92x |
| tail-skewed / 100000 | scan_narrow | 10000 | 0.2399 | 0.2805 | 0.86x |
| tail-skewed / 100000 | scan_wide | 10000 | 0.7372 | 0.7537 | 0.98x |
| tail-skewed / 100000 | filter_adv | 290 | 0.2521 | 0.2799 | 0.90x |
| tail-skewed / 100000 | expression_region | 4813 | 0.2080 | 0.2536 | 0.82x |
| dense, cap 137 / 10000 | scan_narrow | 10000 | 0.3406 | 0.2787 | 1.22x |
| dense, cap 137 / 10000 | scan_wide | 10000 | 0.8196 | 0.5046 | 1.62x |
| dense, cap 137 / 10000 | filter_adv | 362 | 0.3308 | 0.2800 | 1.18x |
| dense, cap 137 / 10000 | expression_region | 5197 | 0.2940 | 0.2565 | 1.15x |

Dense narrow/wide scans benefited from four workers. One worker did not show
the same parallel benefit; more partitions alone added overhead. Sparse narrow
reads and tail-skewed workloads often lost to startup/empty-partition overhead.
Partitioning allocated IDs does not balance actual rows. All partition/worker
configurations and raw repetitions remain in the JSON reports.

## Memory and network limits

Reports record 50ms samples of coordinator-plus-worker RSS, Redis used_memory,
network deltas, per-worker lifetime peak RSS, and first-batch timings. Summed
RSS double counts shared pages and includes coordinator source/oracle and
allocator state from earlier validation. Samples can miss short peaks.
Per-worker lifetime peaks include imports and multiple tasks; they cannot be
summed into a simultaneous peak. INFO monitor traffic is included in network
deltas, and server memory is not attributed exclusively to a query. No hard
per-query memory limit was enforced. These are observations, not a claim
that partitioning always reduces total client/server memory.

## Environment and provenance

macOS 26.7.1 arm64, Mac17,7, 18 logical CPUs, 36 GiB host memory;
Docker server memory limit about 7.75 GiB. Dedicated localhost Redis OSS
8.6.2/Search 8.6.0, no TLS. No paid infrastructure was provisioned.
The task-owned Redis container is removed after validation.
Go 1.27.2; Python and exact Arrow/ADBC/Redis/DuckDB/psutil versions are in JSON.
Binary built at `2571302`; its Go source tree is identical to the later
harness checkouts recorded by the readiness-gated runs. Binary hashes match
the installed-wheel library. This local wheel targets macOS 26 arm64;
it does not validate older macOS versions.

ClickBench PostgreSQL source commit `ccda3033f3c7303d9d7f9634d6dae118ef085f96`.
Official source: <https://github.com/ClickHouse/ClickBench>. Dataset:
<https://datasets.clickhouse.com/hits_compatible/hits.parquet>. Only the first
100000 of 99997497 rows were used, all 105 columns. This sequential prefix is
not representative sampling. Sparse retains every tenth allocated ID; skewed
retains the final tenth. Data/schema licenses apply separately. The source
Parquet is not vendored; its provenance and SHA-256 are in source-prefix.json.
Supplemental queries are not official ClickBench leaderboard SQL.

## Reproduce

Use the commands in [the harness README](../../clickbench/README.md#local-partition-api-comparison).
The gated dense run uses --rows 100000 --repeats 3 --partitions 1 4 8
--workers 1 4. Sparse/skewed runs use --layout sparse/skewed, partitions 1 8
and workers 1 4. The low-cap run uses --rows 10000 after the owner sets the
dedicated server aggregate cap to 137. Automatic cap discovery stays enabled.
Loading/readiness wait is excluded from read timers; initial load_seconds
includes load readiness, while sparse/skewed deletion setup is separate.

Raw diagnostic (fresh output; dedicated disposable server; no settings changed):

```sh
REDIS_URI=redis://127.0.0.1:16390/0 python benchmarks/partitions/reproduce_cursor_duplicates.py --output /tmp/raw.json
REDIS_URI=redis://127.0.0.1:16390/0 python benchmarks/partitions/reproduce_cursor_duplicates.py --wait-index-ready --output /tmp/ready.json
```

## Compatibility audit and remaining #363 criteria

- Refactor commit 96f75c1: six Linux x86_64/arm64 Software single/cluster and
  OSS Cluster jobs passed. Audited native XML: 369 passes, eight feature skips,
  all seven partition tests passed in each. [Run](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39632592892).
- That workflow failed overall: both OSS-single jobs failed earlier when
  DuckDB 2.0.0 could not download community adbc_scanner (HTTP404). This
  follow-up pins analytics CI to the preceding successful 1.5.6 version.
  The existing DuckDB integration passed locally with that version, no skips.
  The follow-up CI outcome is not yet known.
- Prior commit 669fd784: release Software/Cluster and installed Linux wheel
  validation passed on both architectures; macOS wheel build/load passed.
  [Release run](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39628538180).
  Those are historical revision-specific artifacts, not current-wheel proof.
- Current macOS installed-wheel full native validation: 369 passed, eight
  feature skips, three subtests passed (7.64s); all seven partition tests
  passed with a separate named worker denied CONFIG, explicit guaranteed
  cap 137, independent processes and matching binary hash. ACL user removed.
- Cloud, verified TLS, Cloud proxy/cluster routing, older macOS, current
  Linux packaged wheels across the full Python/manager matrix, larger scales
  and #377 runtime handling remain incomplete. GitHub partition-validation
  environment access returned HTTP404; no endpoint/owner was available.
  Workflow declarations are not execution evidence. #363 stays open.
