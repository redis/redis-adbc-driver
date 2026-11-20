# Exact-directory aggregate worker measurements

Date: 2026-11-20. Issue [#551](https://github.com/redis/redis-adbc-driver/issues/551).
Measured source `04091bca456318b463d5a3e8731f2c31ad672320` on base `1d0d0b3b`;
its identical published tree is commit `46d1cbf4d813af69ca161f5d32c0a25dc53c2d5d` after author/committer privacy rewriting. [file hashes/environment](provenance.json)
and [all sanitized samples](results.jsonl). These are local source measurements,
not release, full-scale ClickBench or cold-storage evidence.

## Workload and measurement

Redis Software 8.2.6-58 / Redis 8.8.0, TLS/RESP3, noeviction, one primary shard
per dedicated backend. Each setup reverified active 4x QPF, six effective Search
workers, mnp routing and 32 proxy connections immediately before its profile.
Flex used BigStore v2, 1 GiB total and 256 MiB RAM; its flash path was a container
directory. The client was macOS 26.7.3/arm64, Apple M5 Max, 18 logical CPUs,
36 GiB RAM and Go 1.27.4. The server ran in the shared Linux Docker VM; the
route used Docker's host-published TLS port. Other agent/application work shared
the host. No infrastructure identities, endpoints or credentials are published.

The synthetic table has 8,192 logical source rows with exact BIGINT values beyond
2^53, DECIMAL(30,4) and DOUBLE columns. Three updates and four deletions leave
8,188 winning rows across immutable segments. RAM uses deliberately sparse
native IDs above 2^53, spread over 16 ranges, so it exercises the new authoritative
directory path. Flex uses ordinary contiguous IDs with its explicit plain-index
option. These layouts are different: compare worker settings within a backend,
not RAM versus Flex as a hardware/backend ranking.

The query completely fetches a Go ADBC Arrow result containing COUNT/SUM/AVG over
those columns with `id>=2`. Every sample checks complete values and Arrow schema
against the one-worker result, plus an independent exact count/sum oracle during
setup. Fixed 1, fixed 4 and auto run in that order, with two warmups for fixed
settings and 12 for auto, then five measured queries each. Background workers
are disabled for attribution; separate tests exercise active reads across later
SQL publications and compaction.

Client CPU is process CPU time; allocation is the Go allocation delta. Heap and
server RSS are end-of-query snapshots, not phase peaks. Server CPU/RSS sums the
Redis/proxy processes in the selected dedicated container, including ambient
maintenance. CPU timers have coarse server granularity at this scale. The JSON
also retains process-lifetime client peak RSS, pre-query server RSS, full tuning
observations and post-fetch active/queued counts.

## Observed medians

| Backend | Setting | Actual grants | Completion seconds | Visible rows/s | Client CPU seconds | Server CPU seconds | Client allocated MiB | Client heap after MiB | Server RSS after MiB |
|---|---|---|---|---|---|---|---|---|---|
| Software RAM | 1 | 1 | 0.4759 | 17207 | 0.0337 | 0.03 | 19.96 | 11.69 | 200.68 |
| Software RAM | 4 | 4 | 0.1456 | 56226 | 0.0373 | 0.03 | 20.22 | 12.68 | 201.67 |
| Software RAM | auto | 18 | 0.0505 | 162071 | 0.0442 | 0.03 | 20.70 | 27.81 | 203.92 |
| Software Flex | 1 | 1 | 0.5071 | 16145 | 0.0414 | 0.05 | 19.58 | 13.28 | 230.06 |
| Software Flex | 4 | 4 | 0.1501 | 54536 | 0.0345 | 0.04 | 19.78 | 14.23 | 231.18 |
| Software Flex | auto | 8 | 0.0749 | 109380 | 0.0409 | 0.04 | 19.88 | 13.69 | 232.45 |

Every measured fetch finished with zero active and zero queued internal workers.
This was a single-query workload with no sustained admission contention; it is
not a queue-wait latency benchmark. Separate deterministic tests hold all tokens,
observe queued admission and cancel it without leaked resources. Automatic runs
recorded real scan observations, but the tuner reported a noisy baseline rather
than convergence to a validated optimum. RAM's sparse high sequence allows up to
32 ranges; Flex's ordinary sequence caps this table at eight. The observed auto
grants therefore differ even when the initial learned capacity is the same.

Parallel settings completed this local workload sooner, with similar server CPU
and somewhat greater client allocation. The fixed ordering, warm data, tiny scale,
shared host and macOS published-port route limit this observation. Existing
[transport controls](../point-valid-isolation/README.md) identify route-sensitive
large-reply delays, so these data do not establish Redis engine scaling, a useful
production worker count, general throughput parity or a Flex cold-SSD benefit.
No claim is made about parallel ADBC source partitions or cross-process budgets.

## Reproduce

Use disposable owned Software and Flex fixtures. Start RAM using
`make -C go redis-up` and verify the same effective 4x QPF settings. Set up the
actual compatible Flex fixture through `go/flex-setup.sh`, verifying its product,
BigStore and QPF settings separately. Keep its private environment files local.
Run the opt-in `TestSnapshotDirectoryAggregateProfile` on one endpoint at a time
with `REDIS_DIRECTORY_AGGREGATE_PROFILE=true`, `REDIS_SNAPSHOT_URI` set to that
endpoint, and `REDIS_FLEX_BENCH_CONTAINER` set privately to the owned container.
Set `REDIS_SNAPSHOT_FLEX=true` only for the actual Flex run. Use `-count=1 -p 1`;
the harness clears the selected database and indexes. The source profile is
[here](../../go/snapshot_directory_aggregate_profile_test.go).
