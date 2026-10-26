# Wide Arrow transfer: matched PostgreSQL and ClickHouse references

Measured 2026-10-22 for [#191](https://github.com/redis/redis-adbc-driver/issues/191).
Driver baseline: `aab9612`. **Production driver code is unchanged.**
The two screened experiments did not improve elapsed time and remain patches.
[Manifest](manifest.json), [architecture assessment](architecture.md),
[change records](../../../docs/sdlc/changes/wide-transfer-442/intent.md).

## Matched wide results

Same independently typed official ClickBench prefix: **100,000 rows × 105
columns**, 79,578,019 logical Arrow bytes after canonical validation. Each engine
returns all selected values, normalizes to the same Arrow schema and writes IPC
streams inside timing. Full-value and duplicate-multiplicity comparison is
outside timing. One warm-up and five measured trials per engine/mode/reader
count; forward/reverse method order alternates between rounds.

| Engine / API | Persistent, one reader | Persistent, four readers | Spawned, one reader | Spawned, four readers |
|---|---:|---:|---:|---:|
| Redis ADBC regular / public partitions | 3.308 s | 1.493 s | 3.491 s | 1.874 s |
| PostgreSQL ADBC COPY | 0.352 s | 0.195 s | 0.614 s | 0.525 s |
| ClickHouse uncompressed ArrowStream | 0.199 s | 0.147 s | 0.351 s | 0.356 s |

Persistent wide ranges, minimum–maximum of five trials:

| Engine | One reader | Four readers |
|---|---:|---:|
| Redis | 3.273–3.472 s | 1.462–1.617 s |
| PostgreSQL | 0.308–0.461 s | 0.185–0.257 s |
| ClickHouse | 0.157–0.227 s | 0.142–0.162 s |

On this fixture/environment, one persistent Redis reader takes about **9.4×
PostgreSQL's time and 16.6× ClickHouse's time**. Four readers reduce the gap to
about 7.6× / 10.2×. These are transfer-workload comparisons for the versions and
settings below, not product-wide SQL/analytics rankings.

Persistent mode reuses open independent connections and a thread pool; its timer
includes SQL execution, complete fetch, safe Arrow normalization and file output.
Spawned mode includes fresh process creation/imports, connections, fetch, output
and teardown. Redis four-reader timing also includes partition planning:
ExecutePartitions returns four descriptors consumed by ReadPartition. References
use four disjoint ordinal SQL ranges; they do not expose the same partition API.
Redis's persistent planner uses the first idle reader connection before starting
the fetch tasks. PostgreSQL's COPY path still reconstructs Arrow client-side;
ClickHouse emits Arrow IPC from the server.

### First batches are smaller, but not equal across engines

The primary runner requests 4,096-row Redis batches and ClickHouse blocks;
PostgreSQL uses a 4 MiB batch-size hint. These controls do not guarantee equal
output batches. Actual wide first batches are 4,096 rows for Redis, roughly
3,738–3,868 for PostgreSQL, and 4,096–10,000 for ClickHouse (all readers/trials).
Raw trials retain exact first/min/max batch sizes for every reader.

Persistent one-reader first-batch medians are **0.207 / 0.016 / 0.034 seconds**
for Redis/PostgreSQL/ClickHouse. The earlier Redis experiments' default 65,536-row
first batches are not comparable. Smaller batches improve first-result latency;
they do not remove the full-fetch gap.

### An explicit-range control selects different Redis work

[explicit-range-wide.json](explicit-range-wide.json) repeats the full five-round
matrix using four SQL queries with explicit `__rowid` bounds instead of public
Redis partitions. That shape selects Search LOAD-all rather than the optimized
HASH reader: four persistent readers take **3.085 s**, return about 341 MB from
Redis, and issue zero HMGETs. The primary public-partition path takes **1.493 s**
and issues 100,000 HMGETs. This is an execution-shape control, not a regression
introduced here. Reference engines still use their same explicit ordinal ranges.

## Byte diagnostic: bulk binary output avoids protocol overhead

[traffic-wide.json](traffic-wide.json) uses a separate, unthrottled local TCP
proxy process and one measured round after warm-up. It counts application TCP
payload, including protocol framing, not TCP/IP headers. **Proxy elapsed times
are not part of the direct timing tables above.** All outputs validate exactly.

| One persistent reader | Request MB | Reply MB | Total MB |
|---|---:|---:|---:|
| Redis ADBC | 192.896 | 153.157 | 346.052 |
| PostgreSQL ADBC COPY | 0.003 | 110.732 | 110.735 |
| ClickHouse ArrowStream | 0.002 | 79.347 | 79.349 |

Redis repeats field names in 100,000 per-row HMGET requests. The native binary
references send the query once and stream the result. Redis's total payload is
about 3.1× PostgreSQL's and 4.4× ClickHouse's, but its elapsed gap is larger;
bytes alone do not explain it. Client command/reply objects and typed conversion,
server work, sequencing and transport overhead remain relevant. These counters
do not isolate their wall-time contributions.

A fresh [read-only Go diagnostic](current-profile.log) performs three baseline
scans outside the timed matrices: **3.479 GB allocated, 99.11 million allocations,
93 GCs** (about 1.160 GB and 33.04 million allocations per scan). These are
cumulative allocations, not live/peak memory. It uses Go's Arrow allocator while
the native timing library uses its C allocator. The single diagnostic's elapsed
time is not additional statistical speed evidence. [CPU](current-cpu-top.txt)
and [allocation](current-alloc-top.txt) summaries and raw pprof files are retained.

## Two independent screens: neither promoted

Both screens use the existing `compare.py`, regular and public partition APIs,
the default 65,536-row Arrow setting, four spawned partition workers/eight
descriptors, warm-up and three measured trials. They are separate from the
cross-database runner and its 4,096-row/four-descriptor setting.

| Experiment | Regular baseline → experiment | Partition baseline → experiment | Decision |
|---|---:|---:|---|
| Shared field-name boxes + bounded argument arena | 3.414 → 3.446 s | 1.716 → 1.789 s | No elapsed win; reverted |
| Standalone EVAL_RO page of HMGETs | 3.472 → 4.365 s | 1.771 → 2.187 s | Slower; experiment only |

The [argument screen](command-arena-screen.json) reduces regular client CPU
2.632 → 2.216 s and peak worker RSS approximately 272 → 253 MiB, but elapsed
ranges overlap and the medians are higher. Its [patch](command-arena.patch)
retains command count, wire fields, routing and batch bounds; no allocation/CPU
improvement is treated as proof of throughput improvement.

The [standalone bulk screen](standalone-bulk-screen.json) sends fields once per
page using EVAL_RO, then returns the same nested string replies. Regular request
traffic falls **192.896 → 3.147 MB**, while reply traffic stays approximately
153.168 MB. Regular client CPU falls 2.656 → 2.014 s, but Redis CPU rises
1.086 → 1.539 s and elapsed increases 25.7%. Partition elapsed increases 23.5%.
Fewer bytes and less client CPU do not establish a speedup.

The [bulk patch](standalone-bulk.patch) is an immutable-fixture experiment, not a
supported implementation: arbitrary Cluster key slots cannot share one declared
multi-key script, EVAL_RO requires additional ACL permissions, and each page
executes atomically on its server. It is not tested for production lifecycle,
concurrent updates, restricted ACLs or Cluster behavior. It is deliberately not
in the driver. A typed server export is still a separate design, not this Lua
wrapper; see [architecture.md](architecture.md).

## Narrow control

Same source, selecting `watchid, userid, searchphrase`, five measured rounds:

| Engine / API | Persistent, one reader | Persistent, four readers | Spawned, one reader | Spawned, four readers |
|---|---:|---:|---:|---:|
| Redis regular / public partitions | 2.211 s | 0.533 s | 2.365 s | 0.819 s |
| PostgreSQL ADBC COPY | 0.027 s | 0.034 s | 0.271 s | 0.356 s |
| ClickHouse ArrowStream | 0.009 s | 0.011 s | 0.182 s | 0.268 s |

[narrow.json](narrow.json) retains all trials and actual batch sizes. Narrow
Redis still issues 100,000 HMGETs, here for exact BIGINT fields. Process startup
dominates many reference-engine narrow spawned trials. More readers do not
universally help this small result. No narrow optimization is claimed.

## Environment, identities and limits

- Native macOS arm64 clients, 18 logical CPUs / 36 GiB; Go 1.27.2 and the normal
  c-shared release flags/version value. Standalone measurement interpreter:
  CPython 3.13.16, with [pinned packages](requirements.txt). This benchmark
  environment is separate from the project's Python application requirement.
- Three task-owned Docker containers, each capped at four CPUs / 2 GiB:
  Redis 8.6.2 / Search 8.6.0, PostgreSQL 16.10, ClickHouse 25.8.33.6. Image
  digests, binary/source/fixture hashes and build command are in the manifest.
  No container was OOM-killed. No Cloud resources were created.
- Warm Redis HASH/Search data, PostgreSQL heap and ClickHouse MergeTree
  (`ORDER BY tuple()`). Reference tables add an unselected 64-bit ordinal for
  four-reader ranges; no ordinal index was added. All other selected values are
  identical. This does not equalize internal storage layout or ordering work:
  Redis may impose internal row-ID order while references have no ORDER BY.
- Localhost TCP without TLS. RESP3 for Redis; no Arrow or HTTP compression for
  ClickHouse. ClickHouse queries request `max_threads=4`; PostgreSQL uses server
  defaults within its container limit. Clients share host resources without
  per-client quotas. No correctness/profiling suite runs during timed matrices.
- cgroup CPU deltas include whole-container activity and small sampling overhead.
  Persistent RSS is a shared process's lifetime high-water mark across methods
  and validation, not a per-query peak; use the isolated spawned-reader metrics
  or screen metrics for that narrower interpretation. CPU stacks and CPU totals
  must not be subtracted from wall time to claim socket/server wait durations.
- Idle HTTP keep-alive expiry is handled by one read-only SELECT retry before
  any response is delivered. Reconnection stays timed and its count is recorded.
- The wide run predates a failure-cleanup-only change joining all thread futures
  before unwinding connections; [public-runner.patch](public-runner.patch)
  reproduces its exact runner hash from the final runner. The explicit-range
  runner snapshot is reproduced by [ranges-runner.patch](ranges-runner.patch).
- These 100K-prefix, single-endpoint local results establish neither full-scale
  ClickBench SQL rankings, Cluster/Cloud/TLS performance nor production WAN
  speed. No server export speedup is established.

## Validation and retained failures

The three full direct matrices, byte diagnostic and two screens contain **260
fully validated executions: 50 warm-ups and 210 measured trials**. The loading
and smoke runs are setup, excluded from that count. The primary tables use only
the complete direct public-API matrices. Every retained completed primary trial
compares all selected values and duplicate multiplicity against the independent
source. Harness tests reject changed exact integers, missing/duplicated rows,
schema-nullability loss, and verify HTTP reconnect behavior plus bidirectional
meter counts/clean shutdown.

[failed-attempts.json](failed-attempts.json) separately retains two incomplete
matrix attempts: spawned workers initially lost schema nullability, and later
an expired HTTP keep-alive connection interrupted a run. Both defects were
fixed, regression-tested and the full matrix repeated. Neither failed attempt
is included in the primary timing tables or the 260-execution count.

Production source remains byte-identical to the baseline. Full Redis/Cluster/TLS
driver suites were not rerun for a benchmark-only change, and no such support
claim is made for the experimental patches. Review/check details are in the
[validation record](../../../docs/sdlc/changes/wide-transfer-442/validation.md).

## Reproduce

Use dedicated disposable databases: `compare.py --load` replaces Redis
`transferbench.hits`; `cross_database.py --load` replaces that table in both
references. Follow the [parent fixture instructions](../README.md) to obtain the
official prefix and PostgreSQL DDL. Build `aab9612` using `make -C go build` and
retain the library at an absolute path. Install the standalone requirements.

Start the three cached/pinned images in the manifest with the same CPU/memory
limits and localhost port bindings. Redis uses `--save '' --appendonly no`;
PostgreSQL needs a disposable password; ClickHouse's local container uses
`CLICKHOUSE_SKIP_USER_SETUP=1`. Do not expose these benchmark endpoints remotely.

```sh
python benchmarks/wide-transfer/compare.py \
  --baseline /absolute/baseline.dylib --candidate /absolute/baseline.dylib \
  --uri redis://127.0.0.1:18442/0 \
  --parquet /absolute/hits-prefix.parquet --clickbench /absolute/ClickBench \
  --work /tmp/442-load --load --repeats 1

python benchmarks/wide-transfer/cross_database.py \
  --source /tmp/442-load/source.arrow --redis-driver /absolute/baseline.dylib \
  --redis-uri 'redis://127.0.0.1:18442/0?protocol=3' \
  --postgres-uri 'postgresql://postgres:DISPOSABLE_PASSWORD@127.0.0.1:18443/postgres' \
  --clickhouse-url http://127.0.0.1:18444 \
  --work /tmp/442-wide --load --repeats 5 \
  --redis-container redis-442-bench --postgres-container postgres-442-bench \
  --clickhouse-container clickhouse-442-bench
```

Repeat in new work directories without `--load`, adding:

- Narrow: `--columns watchid userid searchphrase`.
- Explicit-range control: `--redis-layout ranges`.
- Byte diagnostic: `--meter --modes persistent --repeats 1`; keep its timings separate.

The work directory must not already exist. Container-name arguments are optional
and enable Linux cgroup-v2 CPU sampling via Docker outside timing. The byte
meter requires explicit localhost ports. Apply each experimental Go patch to a
separate `aab9612` checkout and build with the same release command; use
`compare.py --repeats 3` without reloading to reproduce each isolated screen.
Do not combine patches or treat them as supported configuration options.

Run harness checks with:

```sh
python -m unittest discover -s benchmarks/wide-transfer -p 'test_*.py'
```

The issue remains open for measured specialized typed decoding/appending and
bounded overlap work. Bulk typed export needs a separate supported server/API
design; this investigation does not select one.
