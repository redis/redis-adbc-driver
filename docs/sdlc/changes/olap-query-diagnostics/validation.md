# Validation

2026-11-26 (UTC), base main `7c83c486`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **Driver diagnostics and pass order** are in every Redis-backed record of the live smoke run. That includes Q15's 60 s warm-up timeout, whose record now names the plan and where the time went ([below](#q15-what-the-record-now-says)).
- **The RAM/flash object split is not available from a Redis-side command on Redis Software 8.8 Flex.**
  - The shard has it, but the database endpoint's proxy does not report it, and scripts may not call `INFO`.
  - Without a sampler the record says `split_available: false`, with key count and the endpoint's `used_memory`. It does not estimate a split.
- **The opt-in external sampler supplies it.** After that finding the user chose this route. With the reference `flex_tier_sampler.py` configured, both live smoke records carry the split (`split_source: "external_sampler"`), and it matched the shard's own counts exactly ([below](#external-sampler-smoke-run)).

## Tests

- **Failing first:**
  - Before `diagnostics.py` existed, `python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py'` ran 65 tests with 13 errors: the new module would not import, and the shared synthetic-worker helper now patches the Flex sampler.
  - Against an empty stub module, the 16 new or changed tests failed (4 failures, 12 errors). That covers every `test_diagnostics` test except the two guards, "RAM profile never sampled" and "imports no engine", which pass trivially, plus the retention assertion in `test_mismatch_has_no_ratio`.
- **Sampler, failing first:** with the hook and reference sampler tests written and nothing implemented, `test_diagnostics` and `test_flex_tier_sampler` had 16 errors:
  - every `SamplerTests` case except the "no sampler, no field" guard;
  - the reference sampler module would not import.
- **After the change:** 81 tests passed, then 93 with the sampler; 1 skipped (the opt-in cgroup test). Both totals were checked in two environments:
  - macOS Python 3.15.2, without `redis-py`;
  - `adbc-olap-release-client:20261123` (Python 3.14, `redis-py` 8.1.0), with `--network none`.
- **Cgroup test:** `test_isolation.WorkerMemoryCgroupTests` with `OLAP_HARNESS_CGROUP_TESTS=1`, in a disposable privileged client container as in CI (512 MiB). OK before and after the sampler.

## Deployment

- **Server:** a dedicated fixture from `go/flex-setup.sh`, started 18:18:30Z and removed 18:44:20Z.
  - Compose project `adbc-olap-diag`, ports 19743/19700.
  - Override: `pull_policy: never`, 20 GiB container limit without swap.
  - Image `redislabs/redis:latest` `679d117a56fc`: Redis Software 8.2.6-58, not re-pulled.
  - Database: Redis 8.8.0 (`8.8-big`), Search 8.8.8, bigstore v2, `search_on_bigstore`, 1 shard, 64 GiB with an 8 GiB RAM tier, `noeviction`, TLS, AOF every second.
  - Search disk settings: 8% / 2% / 3 MiB.
- **QPF:** the management API showed active 4x QPF, 6 Search workers, `mnp` and 32 connections, with no failed check. It was verified at 18:20:00, before the gate (18:32:53), before each query (18:41:44, 18:41:49, 18:42:03) and after the run (18:43:04).
- **Host:** Docker VM, 16 CPUs and 31.46 GiB, running only this fixture and one client at a time.
- **Client:** `adbc-olap-release-client:20261123` (`446def603bd9`), 4 CPUs and 8 GiB without swap, shared client cgroup, 4 client threads.
  - Route: `--network container:redis-adbc-flex`, so the Linux client is in the server's network namespace on a macOS host.
  - The installed v0.0.31 wheel loaded the main `7c83c486` Linux ARM64 library via `REDIS_ADBC_LIBRARY`. SHA-256 `717b20d98815519e1039b4a19f5cf4d0b6e6cceca8dd5f2dda1817f2e02be036`, the library of #696–#717.
- **Data:** TPC-H SF1 from the earlier runs' DuckDB file (SHA-256 `d652d81d…`), copied into the private profile.
  - Loaded 18:23–18:32Z by the unchanged main harness (`run.py --load-only`), all 8,661,245 rows count-checked.
  - The new harness then ran `--load-only --reuse-loaded` into the smoke folder. Its gate passed after 330 s: every index of at least 100 documents had one GC cycle, `nation` and `region` were below the threshold, and `supplier` compacted, so no exemption was used.
- **Cost and ownership:** local Docker only, owned by this session, no cloud resources. The fixture, its network and anonymous volumes were removed with `docker compose --profile flex down -v`.

## Smoke run

One pass: Q11, Q14 and Q15 in one output folder, each with `run.py --reuse-loaded --query N`, as `supervise.py` runs them. The records are in [evidence/queries.jsonl](evidence/queries.jsonl), with [metadata.json](evidence/metadata.json). Query SHA-256s are in [evidence/queries.sha256](evidence/queries.sha256); Q15's matches #717.

| Query | `pass_order` | Redis | Warm-up diagnostics |
|---|---|---|---|
| Q11 | 1, after none | ok, median 714 ms (warm-up 1.12 s) | `execute_returned`, `parallel_streaming_star_join`, grant 32, 64,154 rows, 65,326 commands |
| Q14 | 2, after Q11 | ok, median 1.83 s (warm-up 7.52 s) | `execute_returned`, `parallel_streaming_star_join`, grant 32, 275,983 rows, 278,818 commands |
| Q15 | 3, after Q11, Q14 | error: `TIMEOUT … failed to read snapshot row directory … 57014`, 60.3 s | `execute_failed`, `partition_fetch` ([below](#q15-what-the-record-now-says)) |

**`redis_flex_tiers_before`:** captured after the DuckDB reference validated and right before the Redis worker started. Each sample shows `keys` 8,661,322 (rows plus driver metadata), `endpoint_used_memory` 13.34–13.35 GB, `ram_objects`/`flash_objects` null and `split_available: false`.

For comparison only, outside `run.py`, at 18:43:27Z the shard itself reported:
- `bigdb0:keys=8661322,clean=6938217,dirty=865173,disk=8190741,ram=7803390,meta=349221`;
- `used_memory` 13,444,720,436, `used_ram` 7,069,891,984 of `max_ram` 8,589,934,592.

So the endpoint's `used_memory` tracks the shard's RAM-plus-flash total, not the RAM tier. The shard's `ram` and `disk` object counts overlap.

**Sanitization:** the three records contain no URI, password, endpoint, port, key prefix, index name, `search_query`, `sql_limit` or residual expression. Column names appear only in the existing `flex_index_compaction` gate provenance, which is unchanged. No validation failed, so nothing was retained, and each per-query directory with its raw sidecar was removed.

### Q15: what the record now says

#717 could not say why Q15 times out on this fixture. Its warm-up record now shows:
- **Plan:** `partitioned_exact` with `index_filter: false`, `residual: true` and one group expression, consistent with the revenue CTE grouped by supplier. The Search query was empty or `*`. The CTE's only predicate is its `l_shipdate` range, so (an inference: the record keeps no expression) that range was evaluated as a residual on fetched rows, not pushed into the index.
- **Reads:** `processed_rows` 5,705,472 of `lineitem`'s 6,001,215 when the deadline fired, and `matched_rows` 214,810. That is 92 `row_scan_partitions` of 65,536 rows.
- **Redis:** `redis_commands` 5,730,585, about one command per row read. `redis_round_trip_worker_seconds` 1,778 in 60 s of execute, so about 30 of the 32 granted workers were waiting on Redis throughout. `partition_fetch_worker_seconds` was 1,789.
- **Bytes:** 518 MB read, 1.37 GB written (RESP, excluding TLS); 13.6 GB allocated in the Go process.

This is one sample on one fixture, not a diagnosis of #717. It names a candidate dominant cost: per-row reads of almost all of `lineitem`, without the date predicate in the index query. Confirming it needs the driver change and rerun that #717 describes.

## Limitations

- The DuckDB-over-ADBC route records why it has no diagnostics, not a measurement. The smoke run did not include it; its unavailability and its split sample are covered by real child-process and coordinator tests.
- Only the warm-up is instrumented. A timed run that fails after a passing warm-up keeps the warm-up's diagnostics only.
- The diagnostics cover execute, not Arrow fetch.
- A coordinator killed by the supervisor's outer limit writes a failure record from its checkpoint, which has `pass_order` and the split sample but not the sidecar.
- Not live-tested: a killed Redis worker's last `running` sample, an option refusal by an older driver, and an unreadable sidecar. Synthetic child-process tests cover each.

## External sampler smoke run

The first smoke run showed that the endpoint gives no split. The user then chose an external sampler, so a second fixture was brought up to check it live.

- **Fixture:** same shape and settings as above. Started 20:03:24Z and removed 20:22:58Z, local Docker only, database uid 2.
  - The management API showed active 4x QPF, 6 Search workers, `mnp` and 32 connections. That was checked at 20:03:57, before the gate (20:13:50), before each query (20:22:10, 20:22:16) and after the run (20:22:32), with no failed check.
- **Data:** TPC-H SF1 loaded again, 20:04–20:13Z, by the unchanged main harness.
  - The new harness's gate passed after 285 s. `supplier` (10,000 documents) had no cycle yet and was `exempt_uncompacted` under the profile's existing exemption, with its `FT.CREATE` time and point-term profile recorded.
- **Profile:**
  - `flex_tier_sampler` was `["python", "/harness/flex_tier_sampler.py", "/private/management.json"]`.
  - The owner-only `management.json` named `https://localhost:9443` (the API inside the server's network namespace), the fixture's admin user, `bdb_uid` 2 and `verify_tls: false` for its self-signed localhost certificate.
- **Run:** Q11 and Q14 in one folder; records in [evidence/sampler/queries.jsonl](evidence/sampler/queries.jsonl), with [metadata.json](evidence/sampler/metadata.json).
  - Both validated: Redis medians 763 ms and 1.86 s.
  - `pass_order` was 1 and then 2 after Q11. The warm-up diagnostics matched the first run's plans and row counts.
  - Each `redis_flex_tiers_before` has `split_available: true` and `split_source: "external_sampler"`: `ram_objects` 7,308,809 and `flash_objects` 8,106,468, with clean 6,324,961 and dirty 983,848.
  - The sampler took 35 ms and 34 ms, and its intervals ended at the capture second (20:22:11, 20:22:16).
- **Out-of-band check** ([evidence/sampler/out-of-band-comparison.txt](evidence/sampler/out-of-band-comparison.txt)):
  - Just before each query, `shard-cli 1 INFO Bigredis-Keyspace` inside the server container gave `ram=7308809`, `disk=8106468`, `clean=6324961` and `dirty=983848`. Those are the record's values exactly.
  - At idle afterwards, a standalone sampler run fell between two shard reads with identical counts (`ram=7324211`, `disk=8106468`) and matched them.
  - So the management API's `bigstore_objs_ram` / `bigstore_objs_flash` / `bigstore_objs_clean` / `bigstore_objs_dirty` are the shard's `ram` / `disk` / `clean` / `dirty`.
- **Sanitization:** the records contain no password, user, URI, API host or port, configuration path or `flex_tier_sampler` command.

The sampler's failure paths (non-zero exit, timeout, invalid or oversized output, missing executable, malformed option) are covered by real-subprocess tests, not live.
