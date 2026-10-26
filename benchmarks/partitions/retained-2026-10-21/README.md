# Local retained/live measurements

Measured 2026-10-21 America/Los_Angeles (UTC timestamps may be October 22), by
Codex for Albert Wong. These are owned local macOS ARM64 / Redis 8.10.2 with
Search 8.10.1 measurements, not Cloud or production scores. RESP3 is retained.
Python 3.13.16, PyArrow 26.0.0, ADBC manager 1.12.0 and redis-py 8.1.0 were used.
Each JSON records exact versions, platform, source base `9510b9d`, dirty-tree
status and the measured native library SHA-256. Runtime sources were awaiting
this PR; later review changes only affect connection closure/aborted cleanup,
not these successful fixed-data executions. No paid resources were created.

## Synthetic source matrix

42 runs × 4 combinations = **168 fully drained, exactly validated reads**.
Every source shape/mode has three independent runs. Requested partitions 1/4
and process workers 1/4 are crossed. The actual returned chunk count is recorded;
small retained outputs can yield one chunk even when four were requested.

Inputs: 10,000 allocated IDs with payload widths 0 and 1,024 bytes, each dense,
sparse (keep every tenth ID) and skewed (keep the final tenth); plus 100,000
dense IDs with a 64-byte payload. Both live and retained modes are measured.
Every result is compared by exact sorted values and physical Arrow schema
against a fully fetched ordinary query on the same fixed data.

Representative medians (seconds):

| Input | Live, 1 worker / 1 partition | Retained, 1 / 1 | Live, 4 / 4 | Retained, 4 / 4 |
|---|---:|---:|---:|---:|
| 10k, zero-byte payload | 0.1758 | 0.1875 | 0.1844 | 0.1909 |
| 10k, 1,024-byte payload | 0.1935 | 0.2200 | 0.1829 | 0.2334 |
| 100k, 64-byte payload | 0.4146 | 0.5367 | 0.2952 | 0.5670 |

See [summary.json](summary.json) for all 56 groups and raw per-run JSON for
throughput, planning time, lifetime worker/coordinator RSS, network counters and
server memory samples. Timing includes fresh process startup, connection,
planning, full fetch, IPC writes and teardown. Ordinary query timing omits IPC
and process startup, so it is a different end-to-end workload, not a pure Search
speed comparison. RSS is a process high watermark; server memory/counters are
server-wide before/after samples and not attributed peak allocation.

The 10k cases use defaults. The 100k retained case required explicit limits:
transport 512 MiB, source 256 MiB, working 1 GiB and 10 million work credits.
An earlier 128 MiB transport run failed with SQLSTATE 53200 and published no
result; it is not included as a successful throughput sample. The diagnostic
now names the exhausted allowance. These are logical cumulative credits, not
RSS bounds. No server policy, server memory setting or default limit was raised.

Reproduce from the repo root, with an owned `REDIS_URI` and built native library:

```sh
go/validation/.venv/bin/python benchmarks/partitions/run.py \
  --driver "$PWD/go/build/libadbc_driver_redis.dylib" \
  --rows 10000 --width 1024 --layout dense --partitions 1 4 --workers 1 4 \
  --mode retained --output /tmp/retained-dense-10k.json
```

Repeat three times with fresh output filenames; substitute `live`, `sparse`,
`skewed`, widths 0/64 and row count 100000 as above. Add the recorded JSON
`--execution-limits` override only for the 100k retained case. Each run drops its
UUID-owned source table and verifies its UUID-owned result namespace is empty.

## Reduced ClickBench comparison

[clickbench-1000.json](clickbench-1000.json) contains **90 fully validated reads**:
six workloads × three repeats × ExecuteQuery plus four partition/worker
combinations. All 105 official columns are ingested from the first 1,000 rows
of the previously downloaded official ClickBench prefix. Upstream commit and
input-file SHA are in the report. The independent typed-source/DuckDB oracle
checks complete values by column ordinal; generated expression labels differ
between engines. Every partition's complete ADBC schema also matches ExecuteQuery.

Official SQL Q01 (COUNT), Q03 (SUM/COUNT/AVG), Q05 (COUNT DISTINCT), Q07
(MIN/MAX dates) is unchanged except schema qualification. Supplemental narrow
and wide scans are labeled separately. At 1 worker/1 requested partition,
retained medians were 1.24×, 1.28×, 1.09×, 1.20×, 1.31× and 1.33× the corresponding
ExecuteQuery time. The sampled process-tree/server peaks and network/command
deltas are in every trial. Process-tree RSS double-counts shared pages and
includes source/oracle state; 50 ms sampling can miss short transients.

The admission sweep published 36 of 43 official query results; seven failed the
explicit function policy. Only the six measured workloads have full independent
value/schema oracles in this pilot. Do not extrapolate either result to all
ClickBench SQL, full-scale data or every deployment. In particular, 100k × 105
columns is outside these default source limits and was not timed here.

```sh
REDIS_URI=redis://127.0.0.1:16485/0 go/validation/.venv/bin/python \
  benchmarks/clickbench/partition_profile.py \
  --driver go/build/libadbc_driver_redis.dylib --upstream /tmp/ClickBench-374 \
  --parquet /tmp/hits-prefix-374.parquet --rows 1000 --repeats 3 \
  --workers 1 4 --partitions 1 4 --partition-mode retained \
  --workloads Q01 Q03 Q05 Q07 scan_narrow scan_wide \
  --output /tmp/retained-clickbench.json
```

The pilot drops its owned schema/table and verifies result-namespace cleanup.
Retained execution provides global SQL and immutable replay at an explicit
coordinator/upload/storage cost. These measurements show no general speedup.
