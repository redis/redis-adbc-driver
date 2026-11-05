# Experimental Redis Flex stopgap

Implementation for [issue #322](https://github.com/redis/redis-adbc-driver/issues/322).
Enable database option `adbc.redis.flex=true` together with the usual `uri`.
The default remains the sortable-index / aggregate-cursor execution mode.
Current main requires RESP3; this PR does not add RESP2 connection support.
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

2026-11-05: disposable local Redis Software **8.2.6-25**, Redis **8.8.0**,
Search **8.8.8**, Linux aarch64. BigStore v2, 1,024 MB total / 256 MB RAM,
one shard, search_on_bigstore=true; flash is a container directory. The initial-worktree test
endpoint had no TLS. The current-main PR port was additionally validated with
authenticated TLS and active verified 4x QPF (6 Search workers, mnp routing,
32 connections). No Cloud Flex deployment was tested. Older Software
8.2.0-78 (Redis 8.6.2/Search 8.6.11) lacks plain NUMERIC and cannot use this mode.
The version matrix is evidence for these named builds, not a promise about all
newer Software releases or Cloud products.

From `go/`:

```sh
source build/flex-test.env
go test -count=1 -p 1 -run '^TestFlex' -v .
```

Initial worktree tests covered RESP2 and RESP3. The PR port follows current
main’s RESP3-only policy and reruns live coverage with RESP3. Tests cover SQL
creation/insertion/update/deletion; exact
BIGINT/DECIMAL values; NULLs; 2-versus-10 ordering; NaN/infinities; grouping and
scalar aggregation; 2,500-row scans; sparse IDs and a 2^53 upper boundary;
forced splitting; injected lower server caps, errors and warnings; cancellation;
concurrent inserts/updates against the documented boundary; restricted SQL;
and DROP ownership. Capability-probe key count is unchanged after cleanup.

Comparative benchmarks and expanded validation are recorded in the
[measured report](../benchmarks/redis-flex/2026-11-05/README.md) and
[validation matrix](sdlc/changes/322-flex-stopgap/validation.md). The stopgap saved
about 55% of index memory in the synthetic comparison but was substantially
slower for broad reads, sorting and aggregation. It remains opt-in.

Three-shard Software Flex correctness and race checks were run with authenticated
TLS and verified 4x QPF. Full native ADBC validation was run on RAM and Flex;
The initial Flex run had failures for restricted retained/schema
features; follow-up retained and rekey results are recorded separately below. The native cancellation test asserts zero aggregate cursors in Flex
mode without skipping its cancellation/reuse checks.

A temporary Cloud Pro Flex deployment in GCP us-west2 (Redis 8.6.2) authenticated
over TLS/RESP3 but rejected FT._LIST. The driver refused with NotImplemented and
left the key count unchanged. Cloud SQL support was therefore not established.
The temporary Cloud database and subscription were deleted and removal verified.

A Software database restart validation exceeded its two-minute operator deadline;
restart recovery and HA failover remain unproven. Cold/flash-dominant workloads,
concurrent throughput, general sort spilling and document/flash read counters also
remain follow-up work. See the reports for measurement limits and exact results.

## Software Flex follow-up

Work on #324–#328 and #330 targets Redis Software Flex. Cloud-only #329 is
excluded at the requestor's direction. ALTER/CREATE INDEX work (#325/#326) is
skipped because existing paths require FT.ALTER, unavailable on Software Flex.
See the [current follow-up record](sdlc/changes/324-330-flex-followups/validation.md)
for actual runs; historical measurements above remain dated evidence.

The [Software Flex follow-up performance report](../benchmarks/redis-flex/2026-11-05/followups/README.md) compares 1,000- and 10,000-row page ceilings and records concurrent reads on a flash-dominant working set. Larger pages reduced broad-query latency but increased some heap peaks. For wide values, configure `adbc.redis.stream_batch_bytes` as well as row batching; the measured 8 MiB Arrow budget reduced client heap, but it is not a total-query memory limit. Full sorts still materialize results and have no general spill implementation.
