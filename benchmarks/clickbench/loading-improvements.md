# Bulk loading improvements

Implemented on October 8, 2026. The active cloud benchmark retains its original
build; these changes apply to future loads.

The driver stores an Arrow batch's row values in one backing allocation, resolves
common integer/string array types once per column, and boxes stable Redis HASH
field names once per batch. Existing coercion, owned string storage, defaults,
NOT NULL/CHECK constraints, row-ID allocation and concurrent-DDL checks remain.

The ClickBench loader supports `--load-workers 4` or `--load-workers 8`. Each worker
owns a connection and cursor; source preparation overlaps writes with at most two
batches queued per worker. Running writes finish and queued writes cancel before
cleanup on failure or deadline. Query timing starts after all workers finish and
index/count verification completes. The default remains one synchronous writer;
`--load-prefetch` explicitly enables overlap with one worker.

Parallel row IDs do not preserve source order, so parallel loads cannot use the
existing contiguous-prefix resume check. Resume rejects them instead of risking
duplicated or skipped source data. Run metadata includes worker count, batch size,
prefetch bound and binary SHA-256.

## Local pilot evidence

Apple M4 host, isolated Docker Redis 8.6.2, one Redis instance,
100,000 official source rows, all 105 columns, batches of 10,000. Three trials per
configuration; table values are medians. Timing includes DDL, source decoding,
ingestion, index readiness and count verification. Each trial also passed
source-derived aggregates and 100 complete ordered rows against DuckDB, outside
the loading timer. This is not a full-scale ClickBench result or a measured cloud
speedup. Timing variability and a single-node server limit conclusions about
scaling across the 40-shard Cloud deployment.

| Experiment | Configuration | Median seconds | Throughput versus its control |
|---|---|---:|---:|
| Serial driver changes | Previous driver, synchronous | 13.912 | 1.00× |
| Serial driver changes | Updated driver, synchronous | 13.015 | 1.07× |
| Worker/prefetch sweep | Previous driver, synchronous | 14.259 | 1.00× |
| Worker/prefetch sweep | Updated driver, 1 worker(s), prefetch | 15.679 | 0.91× |
| Worker/prefetch sweep | Updated driver, 4 worker(s), prefetch | 12.626 | 1.13× |
| Worker/prefetch sweep | Updated driver, 8 worker(s), prefetch | 14.452 | 0.99× |

Four workers improved this pilot; eight did not. Single-worker prefetch regressed,
so it remains opt-in. The serial default benefits from the driver changes without
introducing this additional buffering/connection overhead.

Raw pilot artifacts are retained in [profiles/serial-loading.json](profiles/serial-loading.json)
and [profiles/worker-loading.json](profiles/worker-loading.json), including binary hashes.

## Validation

- `go test ./...` passed without a server.
- `REDIS_URI=redis://127.0.0.1:26379/0 go test -p 1 ./...` passed on Redis 8.6.2.
- The benchmark harness's 12 Python checks passed, including bounded prefetch,
  worker ownership, failure/consumer-stop cleanup and retained-prefix slicing.
- Typed conversion checks match the previous path, including numeric overflow,
  NULLs, strings, bounded CHAR, date and unsigned fallback handling; ingested
  strings remain valid after the Arrow buffer is changed/released.
- Four concurrent ingestions produced 8,004 distinct, contiguous row IDs.

Local test containers were removed after validation. No cloud sizing, persistence,
index policy or currently running benchmark process was changed.
