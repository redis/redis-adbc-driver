# Experimental Redis Flex stopgap

The opt-in `adbc.redis.flex=true` mode supports managed-table statement
snapshots on compatible Redis Software Flex deployments. It remains experimental
and requires RESP3, plain NUMERIC/TAG Search support and noeviction. See the
[current implementation and validation](sdlc/changes/flex-snapshots/validation.md).

## Statement-snapshot implementation

Flex snapshot reads traverse exact immutable row-ID directories and retrieve
HASH values. Newest versions and tombstones are resolved before predicates,
sorting, aggregation and SQL limits. This applies to single-segment baselines
as well as overlays; the sorted Search candidate path is disabled. Filtering,
sorting and aggregation execute in the driver. Broad reads can therefore cost
more than the earlier Search-filtered stopgap; no performance parity is claimed.

Final plain indexes retain `SKIPINITIALSCAN`. After private row images are
complete, bounded guarded row-ID HSET notifications populate the fresh index.
Publication requires readiness, no indexing failures, exact prefix/cardinality
and a warning-free indexed-row count. Flex writers require FT.SEARCH permission
for this proof; the RAM write-only initial-scan fallback is unavailable.

Compaction creates its final index before copying complete images. If a populated
private output has lost its index, the driver retires that job and rebuilds fresh
output from retained immutable sources. It does not initial-scan Flex or replay
uncertain mutations. Row-ID allocation uses snapshot metadata copy-on-write and
an atomic ceiling of 2^53. DROP/replace and GC preserve pinned generations and
use registered reference-owned data, rather than legacy prefix deletion.
Managed physical prefixes remain immutable driver-owned namespaces; direct
application writes inside them are unsupported.

Existing stopgap exclusions remain: adoption, application/foreign indexes,
materialized views, CREATE INDEX and general ALTER are unsupported. Explicit
rename/rekey, TRUNCATE, Arrow ingestion and live snapshot partitions retain
snapshot semantics. Reader-only retained-result replay keeps its independent
contract. Cloud compatibility and current restart/failover coverage are not
established by the focused snapshot validation.

## Historical upstream stopgap

The remainder records the pre-snapshot implementation and its dated evidence.
Its mutable-source consistency and indexing descriptions do not describe the
statement-snapshot implementation above. Historical measurements have not been
rerun or relabeled.

Implementation for [issue #322](https://github.com/redis/redis-adbc-driver/issues/322).
Enable database option `adbc.redis.flex=true` together with the usual `uri`.
The default remains the sortable-index / aggregate-cursor execution mode.
RESP3 is required.
Selecting Flex mode is explicit: it also exercises the fallback on RAM databases.

```go
options := map[string]string{
    "uri": "rediss://<user>:<password>@<host>:<port>/0",
    "adbc.redis.flex": "true",
}
```

## Capabilities and scope

Before initializing driver metadata, the opt-in mode creates a uniquely named
probe index with plain NUMERIC row IDs and numeric columns, and TAG strings with
SEPARATOR, CASESENSITIVE and INDEXEMPTY. It writes one temporary HASH, verifies
numeric filtering and HASH retrieval, and deletes both objects. Failed probes
fail the connection; no successful connection is advertised on an empty reply.
Probe creation, deletion and HASH read/write permissions are required in addition
to normal driver permissions. The default mode's legacy missing-index guard is
not a positive Flex compatibility check; do not use default-mode connection
success as evidence of Flex support (see also #314).

Driver-owned tables use no SORTABLE or UNF fields. Numeric/TAG filtering remains
in Search, with existing SQL residual checks. Projection loads authoritative
HASH values. Sorting, top-K, scalar and grouped aggregates run in the driver;
COUNT(*) is also kept off the aggregate-cursor path. No FT.AGGREGATE, WITHCURSOR
or FT.CURSOR is needed by this reader, including string-level checks.

The initial restrictions are deliberate:

- Driver-created tables only. Adoption, application indexes, foreign-index
  discovery and schema rewrites are rejected. Copying into a fresh
  driver-created table remains the alternative to adoption.
- ALTER TABLE schema changes, CREATE INDEX and materialized views are unsupported.
  Rejected SQL operations return ADBC NotImplemented. A single table rename with
  `adbc.redis.rename_rekey=true` uses the bounded Flex reader and copies HASH fields
  into a fresh physical generation, preserving the existing lease/recovery contract.
  It does not use DUMP/RESTORE: rollback of restored Flex HASHes was reproduced to
  remove source Search memberships while source HASHes still existed.
- TRUNCATE switches to a fresh empty prefix/index and cleans canonical owned rows
  without DD. Unrelated keys remain intact; RESTART IDENTITY resets the row sequence.
- No validation of indexing documents that predate an index: new indexes use
  SKIPINITIALSCAN and new driver-owned prefixes.
- Row IDs must be positive integers at most 2^53. This limit applies to hidden
  IDs, not user BIGINT/DECIMAL values, which retain their exact HASH values.
  Allocation beyond the limit fails atomically before advancing the sequence
  or writing rows, so existing rows remain readable. Writer ACLs require EVAL,
  GET and INCRBY on the metadata-slot keys. Other failed writes can leave gaps.
- No automatic migration of plain indexes to sortable indexes when server
  capabilities change. Plain-index metadata requires opting in on later opens.

DROP TABLE drops the index without DD and scans its permanently reserved prefix.
It deletes canonical positive numeric row keys whose HASH marker matches the key.
Adopted HASHes, non-row keys and unrelated prefixes are preserved. Cleanup is
bounded into pipelines; retry DROP after interruption to finish cleanup.

Retained partition execution carries Flex capabilities into the independent,
budgeted source-capture client. Capture uses bounded SCAN/HMGET batches and the
existing schema/cardinality checks; publication and replay retain their existing
limits, ACLs, expiry and cancellation contracts. Reader-only workers consume
published immutable output without Search probes or catalog writes. This does
not add source snapshot isolation. See the [follow-up validation](sdlc/changes/324-330-flex-followups/validation.md).

## Bounded reads and consistency

Each scan captures the allocated upper row-ID boundary and visits disjoint,
inclusive numeric ranges in ascending order. FT.SEARCH NOCONTENT requests at most
10,000 IDs, reduced by request width and any configured lower search cap. A full response or a mismatch
between total matches and returned IDs causes a split before anything from that
range is emitted. Singleton truncation, malformed replies, duplicate keys,
inconsistent markers, warnings and request failures abort the query. Responses
are decoded separately for RESP2 and RESP3. No growing offsets or retries of
already emitted ranges are used. Network failures terminate the reader; callers
may retry the entire SQL statement. Cancellation closes the reader without any
server aggregate cursor to clean up.

At most one range request and one HASH fetch batch are active per reader. The
pending split stack is bounded by the 53-bit ID domain. Fetched rows are sorted by
exact ID before delivery. HASH field size is not byte-bounded. ORDER BY without
LIMIT and some other SQL operators still materialize results; their memory grows
with input size. There is no new general external sort implementation in this
stopgap. Large sorts should be sized accordingly.

This is **not snapshot isolation**. Inserts allocated above the captured boundary
are excluded. Inserts allocated earlier but written/indexed later may appear or
be missed. Updates/deletes and Search visibility can change between ranges or
between Search and HASH retrieval. A row updated after its range was visited is
not revisited; a deleted HASH is skipped. On unchanged, fully indexed data,
ranges return every matching row once. Concurrent changes to row keys/markers
outside the driver are unsupported. Only primary reads are used, as before.

## Validated deployment and evidence

The stopgap is merged in [PR #323](https://github.com/redis/redis-adbc-driver/pull/323),
with retained replay, TRUNCATE, rename/rekey and bounded-page improvements merged
in [PR #331](https://github.com/redis/redis-adbc-driver/pull/331). The measurements
below were recorded on 2026-11-05; this documentation refresh does not rerun them.

The follow-up tested driver source `a121842` on Redis Software **8.2.6-25**,
Redis **8.8.0**, Search **8.8.8**, Linux aarch64, authenticated TLS/RESP3,
noeviction and verified active **4x QPF** (6 Search workers, mnp routing,
32 connections). The full Flex suite used three primary shards on one node,
16 GiB total / 4 GiB RAM. Warm comparisons used separate one-shard RAM/Flex
fixtures; Flex had 1 GiB total / 256 MiB RAM. Flash was a container directory.
Older Software 8.2.0-78 (Redis 8.6.2/Search 8.6.11) lacks plain NUMERIC and cannot
use this mode. These named builds do not establish compatibility for every
Software release or Cloud configuration.

| Validation at the follow-up checkpoint | Recorded outcome | Boundary |
|---|---|---|
| Full native ADBC on Software RAM | 565 passed, 0 failed, 8 skipped; 3 passing subtests | Default RAM regression coverage |
| Full native ADBC on three-shard Software Flex | 559 passed, 6 failed, 8 skipped; 3 passing subtests | Five ALTER cases and one CREATE INDEX case remain failures; no Flex-specific skips were added |
| Data-preserving Software Flex restart | All 25,000 old-reader and whole-query retry rows matched; operator completed in 2.895 s | Replaces the initial operator-timeout result as the later successful checkpoint |
| Planned Software Flex failover | Three nodes, three primaries and three replicas; all three former replicas promoted; all 25,000 rows matched; operator completed in 1.794 s | Docker nodes share one host; physical-host outages were not tested |
| Tiered/concurrent reads | 18 cases, 6.3 million validated rows; dataset exceeded the 256 MiB RAM tier; backing-store counters were positive | Host cache was not flushed and process block-read deltas were zero; physical cold-SSD amplification remains unproven |

See the [follow-up validation](sdlc/changes/324-330-flex-followups/validation.md)
and [sanitized failover evidence](../benchmarks/redis-flex/2026-11-05/followups/failover-validation.json).
An earlier attempt to add replicas to a database populated by the full suite
hit a Search assertion during replica loading. The successful fresh replicated
target does not resolve that server failure or validate every schema for HA.

The [original validation](sdlc/changes/322-flex-stopgap/validation.md) and
[original comparison](../benchmarks/redis-flex/2026-11-05/README.md) remain dated
baseline evidence, including their earlier failures. Initial worktree tests also
used RESP2; current supported connections and follow-up runs use RESP3.

For focused live checks from `go/`, use a disposable Software Flex fixture whose
active 4x QPF and policy have been verified:

```sh
source build/flex-test.env
go test -count=1 -p 1 -race -run '^TestFlex' -v .
```

## Software Flex follow-up

In the latest matched warm comparison, **Flex stopgap averaged 6.9x RAM-default
latency**, and **RAM stopgap averaged 3.1x RAM-default latency**. These are ratios
of the arithmetic means of seven per-workload median latencies. Individual
workloads vary. The [follow-up report](../benchmarks/redis-flex/2026-11-05/followups/README.md)
provides the formula, raw samples, before/after controls and resource limits.
The original schema comparison saved about 55% of index memory; that is not a
measurement of total server memory or flash allocation.

Larger pages reduced broad-query latency and increased some heap peaks. For wide
values, configure `adbc.redis.stream_batch_bytes` as well as row batching. The
measured 8 MiB Arrow budget reduced client heap but is not a total-query memory
limit. Full sorts still materialize results and have no general spill path.

- [#325](https://github.com/redis/redis-adbc-driver/issues/325) and
  [#326](https://github.com/redis/redis-adbc-driver/issues/326) remain open and
  intentionally skipped: the existing ALTER/CREATE INDEX paths require FT.ALTER,
  unavailable on the tested Software Flex build.
- Cloud-only [#329](https://github.com/redis/redis-adbc-driver/issues/329) remains
  excluded from Software Flex work. An earlier disposable Cloud Pro target
  authenticated but rejected FT._LIST, so Cloud SQL was not established. Its
  database/subscription were removed; PR #331 created no Cloud resources.
- Physical-host outage behavior, cold physical-SSD amplification and general sort
  spilling remain outside the recorded evidence. No source snapshot isolation
  or automatic migration to sortable indexes is added.
