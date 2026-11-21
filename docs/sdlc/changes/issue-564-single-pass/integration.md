# Combined managed-ingestion verification

Date: 2026-11-20. Base production source: `1b5918e6`. Candidate: the integrated
changes in this branch. This record covers [#564](https://github.com/redis/redis-adbc-driver/issues/564),
[#565](https://github.com/redis/redis-adbc-driver/issues/565),
[#566](https://github.com/redis/redis-adbc-driver/issues/566),
[#567](https://github.com/redis/redis-adbc-driver/issues/567) and the **design-only**
[#568](https://github.com/redis/redis-adbc-driver/issues/568).

## Delivered and reviewed

Eligible managed NOINDEX Arrow appends create their private index before reading
input, avoiding the final notification pass while retaining publication proofs.
Large compactions reuse catalog validation over at most four atomic copy batches;
persisted batch bounds and protocol 17 are unchanged. The opt-in ClickBench loader
uses one long append per worker, bounded row-group readers, cancellation ownership
and bounded independent post-load validation. Unknown publication outcomes preserve
owned data. The multi-shard document specifies contracts and failure gates; it
introduces no distributed writer runtime.

Three subagents implemented and cross-reviewed these changes. Findings about
unknown outcomes, startup/cancellation ownership, exact Arrow types and bounded
validation were corrected. The final independent lifecycle review found no new
actionable issues; eight targeted review checks passed. Agent review is not human
maintainer approval. Material storage and recovery changes need maintainer review
before merging; no merge or release is part of this work.

## Checks

Dedicated Redis Software 8.8.0 RAM and Flex fixtures were created through
`make -C go redis-up`. Management verification established active 4x QPF, six
Search workers, mnp routing, 32 connections, one shard, TLS and noeviction.
Native Linux ARM64 tests ran in the fixture network namespace; the Python test
runner used the Linux shared library built from the combined production Go code
with Go 1.27.4. Runtime noeviction policy verification does not establish
crash-persistence guarantees. Endpoints, fixture identities and raw artifacts
remain private.

- Go unit-only: `env -u REDIS_URI -u REDIS_SNAPSHOT_URI -u REDIS_SOFTWARE_URI
  -u REDIS_FLEX_URI go test -count=1 -p 1 ./...`: PASS; native tests intentionally
  skip when endpoints are absent.
- Software RAM selector `^TestSnapshot`: **373 top-level tests passed,
  74 capability/opt-in cases skipped**, no failures in the final complete run.
- Software Flex selector `^TestSnapshot(Append(Index|Indexed)|Compaction)`:
  **48 top-level tests passed, 6 opt-in cases skipped**, no failures. Covers the
  combined indexing/compaction changes and their fault/ownership regressions.
- Python `python -m unittest discover -s benchmarks/clickbench -p 'test_*.py' -v`:
  **156 tests run, 153 passed, 3 existing Linux systemd-isolation tests skipped**.
  All three native cases executed: serial, four writers and four NOINDEX writers.
  Each loads and validates 23 synthetic mixed-type rows across 105 columns.
  Versions: Python 3.14.10, PyArrow 26.0.1, ADBC driver manager 1.13.0,
  DuckDB 1.5.6. This is correctness evidence, not full-scale throughput evidence.
- Changed Go files pass `gofmt`; full diff passes `git diff --check`.

An initial RAM invocation omitted `REDIS_URI`, which eight existing snapshot
helpers require in addition to `REDIS_SNAPSHOT_URI`; all eight passed after the
fixture environment was corrected. A broad Flex invocation was stopped after
RAM-oriented live-table, SORTABLE/pushdown and other assumptions hit documented
Flex exclusions. It is not counted as a passing Flex suite. The targeted supported
Flex selector above completed successfully; tests were not weakened or skips
added to disguise the unsupported broad invocation.

## Performance and remaining work

The [single-pass control](evidence.md) records twelve fully validated synthetic
Flex observations: four-writer median acknowledged time 1432.557 to 1131.011 ms
for 16,384 total rows, with notifications 64 to zero. The
[compaction control](../issue-565-compaction/evidence.md) records a separate
4096-row maintenance comparison. These are small isolated controls, not canonical
ClickBench results or proof of all-core writes.

Full 100-million-row throughput, sustained client/server memory, cold SSD I/O,
concurrent compaction backlog, reusable backup/restore and all 43 benchmark
queries remain unperformed. Multi-shard runtime work remains gated on supported
coordination and design review. Keep the issues open for their remaining
acceptance evidence. The operational ingestion stayed stopped and no AWS job
was started. Only task-owned disposable test fixtures were used.
