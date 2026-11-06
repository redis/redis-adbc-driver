# Expanded validation and comparative measurements

Date: 2026-11-05. Historical PR #323 validation checkpoint; implementation
merged in [PR #323](https://github.com/redis/redis-adbc-driver/pull/323).
The [PR #331 follow-up](../324-330-flex-followups/validation.md) records later
retained/rekey, restart/planned failover and resource results. Counts and failures
below are preserved from the initial checkpoint; this is not a fresh test run.
See [intent](intent.md), [spec](spec.md), [plan](plan.md), the
[benchmark report](../../../../benchmarks/redis-flex/2026-11-05/README.md), and
[machine-readable results](../../../../benchmarks/redis-flex/2026-11-05/validation-summary.json).

## Final outcomes

| Validation | Outcome |
|---|---|
| Complete unit suite, integration URIs unset | All Go packages passed; live cases skip as intended |
| Standard RAM cursor/schema/allocation regression selection | Passed |
| Initial complete Go RAM integration suite | Eleven retained-policy setup failures; all affected groups passed after the owned API verified noeviction and the required declaration was supplied |
| Complete default native ADBC suite on RAM | 565 passed, 8 skipped, 3 subtests passed |
| Complete default native ADBC suite on three-shard Flex | 527 passed, 38 failed, 8 skipped, 3 subtests passed; zero setup errors in the final capacity-adjusted run |
| Live disconnect, stalled socket read, cancellation, and full-query retry | Passed, also under the race detector; failures occurred after 624 rows and retries independently returned all 5,000 rows once |
| Actual Search cap of 2 | Passed: 100 rows returned exactly once; limit discovered through the normal reader path, restored afterward |
| Actual row IDs at 2^53−1 and 2^53, forced adaptive splits | Passed after the allocation-bound fix |
| Concurrent allocation of the final permitted row ID | Passed under race detector: exactly one success, one rejection, no counter overflow |
| Retained mode rejection before Redis I/O | Passed with a deliberately absent Redis client |
| Software database restart | Failed to complete within the two-minute operator deadline; recovery was not established |
| Shared-library build and whitespace checks | Passed |

Full pytest runs used the repository's normal feature selection, without adding
Flex-specific skips or weakening expected SQL values. The final Flex failures are
31 retained/reader-mode cases, 5 ALTER cases, 1 CREATE INDEX case and 1 rename
rekey case. Some assert particular errors/publication behavior within an already
unsupported feature; these still fail and remain visible. This is **not** a clean
full-ADBC compatibility pass for Flex.

The streaming cancellation test now expects no new aggregate cursor in explicit
Flex mode. Its checks for one returned batch, CANCELLED/57014, termination, cleanup
and reuse of the statement remain unchanged. Default-mode cursor assertions remain.

## Capacity and deployment coverage

All local Software targets used 8.2.6-25, Redis 8.8.0, Search 8.8.8,
authenticated TLS/RESP3 and verified active 4x QPF: 6 Search workers, mnp routing,
32 connections. One- and three-shard Flex focused coverage passed. These are
multiple primary shards on one Software node, without replicas; they do not
establish multi-node HA failover.

The first full Flex suite used 1 GiB total / 256 MiB RAM across three shards and
reached a disk write-buffer limit at 17 concurrent indexes. A 4 GiB / 1 GiB retry
reached 68 indexes. This was an actual server resource boundary, not lack of
NUMERIC support. The final full suite used 16 GiB total / 4 GiB RAM, three shards,
and externally verified noeviction. The benchmark fixtures remained separate in
configuration: both one shard, RAM 1 GiB, Flex 1 GiB total / 256 MiB RAM.

Disk-index capacity requires room for both table indexes and the transient
capability-probe index. Increasing the validation fixture removed cascading setup
errors; it did not turn unsupported features into supported ones. The new
FLEX_SHARDS=1|3 setup option makes the deployment topology reproducible. The
public CA is readable by the container user; private-key permissions remain 0600.

A temporary Redis Cloud Pro Flex deployment in GCP us-west2, configured with a
250 GB dataset, authenticated over TLS/RESP3 and reported Redis 8.6.2. FT._LIST was
rejected. Driver open returned NotImplemented and DB key count was unchanged.
The tested Cloud configuration therefore cannot exercise SQL through the stopgap.
The account's API offered versions through 8.6; 8.8 and an explicit Search/QPF
request were rejected in dry runs. This does not establish capabilities of other
Cloud accounts, regions or beta settings.

The Cloud dry run quoted $2.04/hour. The temporary database and subscription were
deleted, removal verified, and private credential/control files removed. The
quote is not an observed billing total. Only a
[sanitized capability result](../../../../benchmarks/redis-flex/2026-11-05/cloud-capability.json)
is committed; endpoints, keys, passwords and infrastructure IDs are omitted.
Task-owned local fixtures were removed after the final checks.

## Fixes driven by validation

- The reproduced boundary case showed that a rejected over-limit insert advanced
  the counter and made valid rows unreadable. Flex now checks/increments the
  counter atomically with the metadata read, within the exact integer domain.
  Rejection leaves the counter unchanged. The standard allocation transaction is
  preserved. This adds no transaction API or snapshot guarantee.
- Resource/permission/transport failures from capability probes retain an I/O
  diagnostic. Only actual unsupported-command/feature replies are treated as
  absent capabilities.
- Retained partition requests reject before Redis I/O/publication in Flex mode,
  rather than failing inside a source capture that omitted the mode. Full retained
  execution remains out of scope.

## Reproduction and remaining gaps

For focused live checks, source the generated Flex test environment in go/ and run:

```sh
go test -count=1 -p 1 -race -run '^TestFlex' -v .
```

The actual-cap and server-restart cases are separate operator opt-ins. See their
REDIS_FLEX_EXPECT_SEARCH_CAP and REDIS_FLEX_RESTART_CONTAINER/DB guards. At this initial checkpoint, restart
validation was not established; later successful operator runs are recorded in
the [follow-up](../324-330-flex-followups/validation.md).

Native validation from go/validation uses REDIS_URI, REDIS_RETAINED_TLS_CA_FILE,
REDIS_ADBC_FLEX=true for Flex, and the externally verified retained-control policy
where needed, then `.venv/bin/python -m pytest -q -r fEsxX tests/`. Keep raw logs
and JUnit files private: failures can contain connection arguments. Published
results contain aggregate counts and controlled categories only.

At this initial checkpoint, HA failover, successful Cloud SQL execution, cold/flash-dominant data, concurrent
throughput, general sort spilling, per-document flash reads and transient server
memory peaks remain unproven. The measured warm workload shows a real index-memory
saving and substantial broad-query latency costs; see the benchmark report.
