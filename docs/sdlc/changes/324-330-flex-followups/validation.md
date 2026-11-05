# Follow-up validation

Date: 2026-11-05. Base main `4ae4700`. Status: in progress. See [plan](plan.md).

Isolated Software Flex fixture: Software 8.2.6-25 / Redis 8.8.0 / Search 8.8.8, three primary shards on one node, BigStore v2, 16 GiB total / 4 GiB RAM, noeviction, TLS/RESP3. Setup verified active 4x QPF, 6 Search workers, mnp routing and 32 connections before tests. Infrastructure identifiers and raw logs remain private. This topology is not HA coverage.

- Focused retained executor mode/admission unit selection: passed.
- Shared-library build through `make build`: passed.
- Native partition/retained files: 15 passed, 1 failed in ALTER TABLE (separate #325). No skips added or assertions weakened.
- Full native suite after retained/TRUNCATE: 558 passed, 7 failed, 8 skipped, 3 passing subtests. All retained replay cases passed. Remaining failures: five ALTER cases, CREATE INDEX, and rekey.
- Live three-shard Flex under race detector: retained mode propagation and TRUNCATE ownership passed.
- Live rekey regression reproduced lost Search membership after DUMP/RESTORE copies were rolled back, while source HASH values remained. Independent HGETALL/HSET copies fixed the reproduced failure.
- Six Flex race checks passed: source HASH/Search rollback regression, existing multi-page rekey, close during copy, close after switch, resumed moves, and takeover wait.
- Software RAM race regressions for retained execution, TRUNCATE and selected rekey lifecycle cases passed.
- Full native suite after rekey fix: 559 passed, 6 failed, 8 skipped, 3 passing subtests. The remaining failures are five ALTER cases and CREATE INDEX; the native close-during-rekey case passed.

Scope: #325/#326 are skipped under Albert's direction because existing paths require unsupported FT.ALTER. The all-suite failures remain visible; no skip or weakened assertion is added to hide them. #329 is excluded because work is now Software Flex only.

Cloud blocker (#329): [current Redis Cloud documentation](https://redis.io/docs/latest/operate/rc/databases/create-database/create-flex-database/#redis-search-on-flex-beta), checked 2026-11-05, describes a Pro subscription beta flag, but explicitly excludes NUMERIC fields, FT.AGGREGATE and background indexing. Plain NUMERIC is required by the stopgap. The previously tested account offered Redis through 8.6 and rejected Search configuration. Enabling the documented beta alone therefore cannot fulfill the SQL acceptance criteria. No new Cloud deployment has been provisioned or successful Cloud SQL claimed in this follow-up.

## Restart and unit checkpoint

The complete unit suite passed with integration URIs unset. Its first sandboxed run could not bind loopback test sockets; rerunning with local socket access passed all packages.

The Software Flex data-preserving restart test passed after supplying the documented global rladmin `-y` flag for noninteractive confirmation. The operator restart completed in 3.155 seconds; the old reader delivered all 5,000 rows with no terminal error, and the independent retry/count/value checks passed. Three primary shards, no replicas: multi-node HA remains a separate test in progress.

## Multi-node planned failover

A fresh three-node Software 8.2.6-25 cluster was configured with three primary shards and three replicas before any Search indexes were created. TLS/RESP3 and effective 4x QPF were verified. The race-enabled TestFlexServerFailover passed: the planned failover completed in 2.886 seconds, the old reader returned all 5,000 rows, and independent count/sum plus every ordered row were validated after failover. Management API comparison confirmed all three former replicas became primaries and all six shards were active. This is planned shard failover on multiple Docker nodes on one host, not a physical-host outage test.

An earlier attempt to enable replicas on the database populated by full native validation failed during replica Search index loading: Search asserted in indexes.c:1369 (`ok` is not true) during LOADING_ENDED. Source HASHes remained available. The stuck configuration rejected API deletion with 409; only task-owned disposable containers were recreated. Raw logs remain private. The fresh replicated target passed, but that success does not erase the populated-database replica-loading failure or establish every schema's HA compatibility.

After validation, the replicated containers were removed and matched single-shard benchmark fixtures provisioned.

## Performance source a121842 and final regressions

The bounded reader's maximum page increased from 1,000 to 10,000, retaining width-based limits, lower server caps and split-before-emission semantics. Matched one-shard Software RAM/Flex trials produced 360 warm samples and 72 groups. All returned fields were independently checked and every Arrow stream consumed. Larger pages reduced broad-query latency and increased some heap peaks; selective Flex timing did not improve. See the [complete performance/resource report](../../../../benchmarks/redis-flex/2026-11-05/followups/README.md).

A separate 150,000-row workload with 4,096-byte varying payloads exceeded the 256 MiB Flex RAM allocation. Eighteen default/8 MiB Arrow-budget cases at 1/2/4 readers validated 6.3 million returned rows. Database statistics confirmed positive flash-fetch/read activity, about 250–260 MB used RAM and 466–474 MB used BigStore. Per-query process block-read deltas were zero; OS page cache was not flushed, so cold physical SSD amplification remains unproven. General sort spilling is unavailable and skipped under the requestor's scope. No default Arrow batch option changed.

- Final complete unit suite and versioned shared-library build: passed.
- Three-shard live Flex/fault/boundary/TRUNCATE/rekey race selection: passed. Fault datasets were increased to 25,000 rows so failures still occur after delivered rows with the larger pages.
- Actual server Search cap of two: passed under race detector; cap restored to 1,000,000 and active effective 4x QPF preserved.
- Three-shard data-preserving restart with 25,000 rows: passed under race detector, 2.895 seconds, old reader returned all rows and full-query retry matched every value.
- Full default native ADBC Software RAM: 565 passed, 0 failed, 8 skipped, 3 subtests passed.
- Full default native ADBC three-shard Software Flex: 559 passed, 6 failed, 8 skipped, 3 subtests passed, zero setup errors. The five ALTER failures and CREATE INDEX failure remain visible; these are the skipped #325/#326 work. No assertions were weakened or Flex-specific skips added.
- Final 25,000-row multi-node planned failover: passed under race detector in 1.794 seconds. All three former replicas were verified promoted across Software nodes, all six shards active, and effective 4x QPF preserved. Every old-reader and retried-query row matched.
- Committed portable profiler wrapper smoke check: passed with 1,000 rows and one measured repetition; these smoke samples are excluded from performance claims.

Raw logs/JUnit, API controls, credentials, endpoints and infrastructure identifiers remain private. Published artifacts include only controlled fixture metadata and numeric results. Task-owned fixtures are removed after final checks; cleanup verification is recorded below.

## Cleanup and review

Removed four task-owned containers and both task-owned Docker networks. Verified no owned containers remained and both pre-existing Software fixtures were still running. No Cloud resources were created in this follow-up. [Sanitized cleanup evidence](../../../../benchmarks/redis-flex/2026-11-05/followups/cleanup.json) is committed.

Self-review covered exact row/value/order behavior, terminal reader failure/cancellation, row-ID precision, persisted rekey layout capability, independent HASH copies, rollback/source ownership, retained source mode propagation and reader-only replay. Existing RAM behavior was checked by the complete native suite and focused race tests. No remaining driver change is required within this task's narrowed scope. This self-review is not maintainer approval; PR review/merge and release remain separate. Known server populated-database replica-loading failure, unsupported schema operations, absent general spill and unmeasured physical-host outages/cold-SSD amplification remain explicit limitations.
