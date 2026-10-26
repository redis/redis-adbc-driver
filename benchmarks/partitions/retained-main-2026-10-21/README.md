# Retained partitions after main integration

Measured 2026-10-21 America/Los_Angeles on the current macOS ARM64 library
containing retained runtime `f9f2dad` and main through `944170f`. The raw report
records the library SHA256, source revision/dirty state, package/server versions,
load/index readiness, memory, network and full-fetch exact oracles. Later changes extend validation and correct foreign-index schema sampling;
the managed-table workloads measured here do not use foreign discovery. These
reports identify the measured revision and are not a rerun of later commits.

[ClickBench report](clickbench-1000.json): 1,000-row prefix, all 105 columns,
three repeats, workers 1/4 and requested partitions 1/4. All **90 measured reads**
matched the independent DuckDB value oracle and physical driver schema. The six
measured workloads are Q01/Q03/Q05/Q07 and narrow/wide scans. Admission probing
of other official queries is not a correctness/performance claim for them.

Single worker / one partition median retained time divided by ExecuteQuery:

| Workload | Relative time |
|---|---|
| Q01 | 1.32× |
| Q03 | 1.27× |
| Q05 | 1.18× |
| Q07 | 1.33× |
| scan_narrow | 1.25× |
| scan_wide | 1.24× |

Retained publication and storage add work; this small local sample does not
show a speedup or establish production throughput. The complete 43-query,
100-million-row official ClickBench run was not performed. No source snapshots
or exactly-once guarantees are implied. Temporary source data and retained
namespace cleanup were verified by the runner.

Command (owned loopback REDIS_URI supplied privately):

```sh
go/validation/.venv/bin/python benchmarks/clickbench/partition_profile.py \
  --driver go/build/libadbc_driver_redis.dylib \
  --upstream /tmp/ClickBench-374 --parquet /tmp/hits-prefix-374.parquet \
  --rows 1000 --repeats 3 --workers 1 4 --partitions 1 4 \
  --workloads Q01 Q03 Q05 Q07 scan_narrow scan_wide \
  --partition-mode retained --output REPORT.json
```

Synthetic rerun: **168 fully fetched reads** in 42 runs, three repeats of
10k allocated IDs with width 0/1024 and dense/sparse/skewed layouts, plus
100k dense IDs with width 64. Both live and retained use requested partitions
1/4 and workers 1/4. Sparse/skewed layouts retain 10% of allocated IDs.
[Summary](synthetic-summary.json); [complete raw JSON archive](synthetic-raw.json.gz).
100k retained runs explicitly use transport 512 MiB, source 256 MiB, working
1 GiB and 10 million work credits. Other runs use defaults.

Representative median end-to-end seconds:

| Allocated rows / width | Partitions / workers | Live | Retained |
|---|---|---:|---:|
| 10000 / 0 | 1 / 1 | 0.1687 | 0.1743 |
| 10000 / 0 | 4 / 4 | 0.1782 | 0.1707 |
| 10000 / 1024 | 1 / 1 | 0.1899 | 0.2124 |
| 10000 / 1024 | 4 / 4 | 0.1802 | 0.2333 |
| 100000 / 64 | 1 / 1 | 0.4077 | 0.5279 |
| 100000 / 64 | 4 / 4 | 0.2837 | 0.5509 |

These are scoped local measurements. Returned partition counts are recorded
separately because retained output may contain fewer chunks than requested.
Server memory peaks are process-lifetime values and include prior fixtures;
process RSS includes interpreter/Arrow overhead. All runs used the same library
hash as the ClickBench report.
