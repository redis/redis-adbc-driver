# Execution diagnostics

Issue #323 adds the optional statement setting
`adbc.redis.execution_diagnostics_file`. The driver atomically replaces this
JSON file once per second during execute, then writes `execute_returned` or
`execute_failed` when execute ends.

```python
cur.adbc_statement.set_options(**{
    "adbc.redis.execution_diagnostics_file": "/tmp/query.execution.json"
})
cur.execute(sql)
```

Snapshots include the actual worker grant, execution phase, scan/grouping plan,
string exactness metadata, residual predicate, pagination caps, pushed LIMIT,
processed/matched rows, and aggregate progress. The sampler reads atomics and
immutable metadata inside the driver; it never calls the busy ADBC handle.
Plans include query literals and column metadata, so sidecars use owner-only
file permissions. Connection credentials and URIs are not serialized from the
configuration, and snapshots contain no result rows.
File failures cannot replace the query outcome. Use a distinct path for each
trial to preserve earlier measurements.

`execute_returned` records the end of execute, not successful Arrow fetch,
independent output validation, or valid LIMIT ties. For streamed results the
sampler stops when execute returns; subsequent fetch time belongs to the
caller's fetch stage.

## Metric scope

- `worker_grant` is the actual grant in the latest execution, zero if none
  was admitted. It is not inferred from process CPU usage.
- `redis_round_trip_worker_seconds` includes pool wait, Redis/network wait,
  and reply decoding. `socket_read_worker_seconds` measures client read waits.
  Both include operations in flight and sum concurrent worker time.
- `connection_read_bytes` and `connection_written_bytes` count RESP bytes on
  that connection during execute, including connection maintenance and
  retries. TLS framing is excluded. `redis_server_profile` remains null;
  client waits do not measure server CPU or isolate a Redis execution stage.
- `processed_rows` counts fetched scan candidates, including disappeared
  HASHes; `matched_rows` counts decoded rows passing residual predicates.
  Partitioned server tuples instead report `represented_rows` in aggregate
  metrics. Failed/replayed attempts can contribute to counters more than once.
- `logical_value_bytes` counts returned tuple field/value content, not wire
  framing. `row_scan_partitions` includes intentionally narrow scans;
  `row_fallbacks` counts retries of attempted server tuple plans.
- Spill read/write bytes include intermediate merge passes. Spill read/write
  seconds measure file I/O; sort seconds measure in-memory sorting.
  `partition_fetch_worker_seconds` includes scan-page decoding/filtering.
  `merge_elapsed_seconds` includes the current merge; the older
  `merge_wall_seconds` field records completed merges only.
- Allocation and heap snapshots cover the Go process, including other
  goroutines and diagnostic work. They are not per-query allocation counts.

Metrics observed before termination are incomplete lower bounds. Concurrent
worker seconds can exceed wall time. The diagnostic format is versioned;
version 1 contains the fields described here.

## ClickBench retention

`trial.py` enables this setting for each trial, writing `qNN-T.execution.json`.
The Python stage sampler and supervisor read that file without issuing ADBC
calls during execute. Killed trials retain its latest snapshot under
`partial_diagnostics.execution_diagnostics`, alongside process CPU, RSS, and
stage timings. Their failure type and null timing remain unchanged. Older
driver versions that reject the option still execute and record the diagnostic
setup failure separately. The runner clears stale sidecars before each trial.

See the [local execution-path report](../benchmarks/clickbench/execution-323/report.md)
for correctness coverage, reproducible profiles, independent validation, and
remaining full-scale acceptance work.
