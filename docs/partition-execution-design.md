# Partition execution roadmap and feasibility

Historical design baseline. The opt-in retained coordinator is now implemented;
see [the current runtime contract](retained-partition-results.md) and
[validation evidence](sdlc/changes/retained-execution-budget/evidence.md).
The proposed values and deferred statuses below describe the earlier design,
not current option defaults or runtime coverage.

Date: 2026-10-21. Source baseline: `edbd299`. Requested by alberttwong in
Codex. This is an engineering design, not a maintainer approval or a claim of
implemented compatibility. See [the change records](sdlc/changes/partition-followups/intent.md)
and [the current API contract](partitioned-reads.md).

## Global and multi-relation SQL — [#166](https://github.com/redis/redis-adbc-driver/issues/166)

The chosen extension path is **bounded retained final Arrow results**, rather
than independently evaluating a complete SQL query on each input range. Only
row-local operators are enabled in this change. No partial aggregate states
are returned as final rows. A new retained mode needs its own descriptor
version; live numeric ranges keep their present behavior.

| Operator | Current deferred mode | Proposed retained mode / semantics |
|---|---|---|
| Columns, literals, arithmetic, casts, CASE, allowlisted scalar functions | Stream each disjoint range; one typed binding | Same evaluator and output schema |
| ORDER BY | Rejected | One global sort; caller must merge ordered output by explicit ordinal, not arrival time |
| LIMIT/OFFSET | Rejected | Apply once after global operators; without ORDER BY chosen rows are unspecified, as in ordinary SQL |
| DISTINCT / DISTINCT ON | Rejected | One global deduplication with ordinary NULL/NaN/collation equality; DISTINCT ON uses the ordinary global ordering rules |
| GROUP BY / aggregates / HAVING | Rejected | Finalize once using the ordinary evaluator; no per-range averages/counts represented as final rows |
| Window / QUALIFY | Rejected | Compute complete windows, including peer ties and NULL ordering, before splitting final rows |
| UNION / INTERSECT / EXCEPT | Rejected | Apply global set or multiset semantics, including ALL duplicates, before splitting |
| Joins | Rejected | Execute once; capture all dependencies, aliases and generation identities |
| CTEs / views / derived or correlated subqueries | Rejected | Resolve transitively once; freeze expansion, bindings and settings; reject volatile or externally changing dependencies |
| Volatile / user / session-dependent functions | Rejected except captured time zone casts | Remain rejected until a defined capture/evaluation policy exists |

Unspecified sort ties must not become an implicit unique key. A retained result
assigns ordinals *after* the ordinary executor finishes its order/limit logic;
retries replay those chosen rows in that order. NULLS FIRST/LAST, NaNs,
decimal precision, collation, duplicates and Arrow field metadata must match
ordinary execution. Unordered readers provide a multiset, not global order.

### Memory and lifecycle gate

The current ordinary executor materializes many blocking operators. Merely
splitting its result afterward does not bound planning memory. Before enabling
retained mode, implement an executor-wide allocation/row/byte budget checked
before allocation and while building intermediates. A proposed default is
256 MiB per job plus 1 GiB total retained output; exceed either with a resource
error before publishing descriptors. These are proposed limits, not options
available today. A real external sort/hash spill implementation can replace
those limits later; no spill is currently provided.

Publish descriptors only after every final batch and its manifest are committed.
Cancellation before commit removes all unpublished data. Reader cancellation
closes only its local reader; early release leaves immutable results available
until job expiry/revocation. Independent worker failure must not mutate a job.

The enabling test gate is exact Arrow schema and sorted multiset equivalence
with ordinary execution for each operator (including empty/all-NULL data,
duplicates and ties), independent authenticated processes after planner closure,
DDL during dependency resolution, cancellation at every publication stage,
early release, budget exhaustion and worker death. Until that exists, retain
clear NOT_IMPLEMENTED errors for the rows above.

## Additional layouts and large IDs — [#167](https://github.com/redis/redis-adbc-driver/issues/167)

| Layout | Safe proposed coverage strategy | Decision / guard |
|---|---|---|
| Managed numeric IDs below 2^53 | Disjoint exact inclusive integer ranges | Implemented; gaps yield empty ranges; IDs are unique within a physical prefix |
| IDs at or above 2^53 | Retain exact key identities (or final Arrow rows); split by ordinal, never numeric Search boundaries | Reject today; do not round or use floating boundaries to claim int64 coverage |
| Adopted/application HASH keys | Retain a unique exact key manifest or final results; partition manifest ordinals | Never assume numeric keys; deduplicate byte-identical keys before publication; empty manifest returns schema and no rows |
| Foreign/application Search index | Retain the exact result of one query, or an exact deduplicated document-key manifest with residual evaluation | Need index-definition/dependency fingerprint and schema validation; arbitrary indexes do not establish unique stable row IDs |
| Legacy metadata without unique prefix | Explicit operator-requested copy/migration to a new managed table, or retained final results | No silent metadata rewrite, adoption or rekey while planning |
| Temporary tables | None until an authenticated independently readable retained result exists | Remain unsupported; a session-local table name is insufficient identity |

SCAN-based key manifests cannot promise point-in-time enumeration under writes:
SCAN may return duplicates and keys may disappear. Deduplication also consumes
state. A live key-manifest mode would freeze membership only, observe current
values on each fetch, and require an explicit maximum manifest size. It is not
a snapshot. Prefer retained final rows for replay; it avoids rechecking an
application-owned index on each retry.

Tests before enabling any layout must prove exact fixed-data coverage, empty
ranges, byte keys, stale schema/index identities, rename/recreation, rekey,
Cluster routing and independent reader ACLs. Capture all application keys,
values, index definitions and metadata before/after planning, reader release
and expiry; assert no mutation, ownership transfer, delete or rename. Only
job-owned retained keys may be removed. This change introduces no such state
and leaves these layouts rejected.

## Stable retry results — [#169](https://github.com/redis/redis-adbc-driver/issues/169)

| Mechanism | Concurrent writes / retries | Costs and feasibility |
|---|---|---|
| Existing live ranges | Freeze allocated high watermark only; updates/deletes and unfinished inserts remain visible; retry may differ | No retained storage; independent reads survive planner closure; keep as default |
| Immutable final Arrow batches + manifest | Freeze the completed query output; retries replay those bytes until expiry | One full coordinator read, upload, retained output bytes, and download per consumer/retry; bounded executor needed |
| Versioned HASHes + read timestamp | Could provide a common as-of dataset if *all* writers and DDL use versioning | Requires write protocol, tombstones, versioned schema/indexes, garbage collection; not compatible with arbitrary application writers |
| Redis persistence backup/replica | Not an independently authorized per-query snapshot contract | Reject as an implementation shortcut; backups/replication alone do not give repeatable per-worker SQL reads |

Choose opt-in immutable final results for the next prototype. It guarantees
replay of the coordinator's completed result, **not** that the coordinator read
one atomic Redis snapshot while writers were active. Concurrent updates and
deletes during materialization may still produce a mixed-time result. A true
as-of snapshot requires a writer-coordinated versioned source or a quiescent
source contract and is a separate feature. Stable replay is also not
end-to-end exactly-once processing: consumers still need transactional sinks
or deduplication/checkpointing.

### Proposed retained-state contract (not implemented)

- Use random job IDs in a dedicated driver-owned namespace; store IPC batches
  separately with checksum, exact schema fingerprint and ordinal intervals.
  Do not include endpoint/password in descriptors or trust a job ID as a bearer
  authorization token. Reading requires independently authenticated access to
  job data and a tenant/job authorization check; define ACLs before enabling.
- Proposed lifetime: one hour from committed publication, no renewal by reads.
  Every batch and manifest has an absolute expiry fixed at commit. A job
  manifest is published last; incomplete uploads are unreadable and expire.
  Redis Cluster multi-key publication is not atomic across slots, so readers
  validate the committed manifest and every referenced checksum/expiry.
- Explicit revoke deletes/marks the manifest first, then removes only job-owned
  chunks. Abandoned coordinators cannot leak immortal chunks: provisional
  uploads must have TTLs from their first write. A cleanup sweep is idempotent;
  no table/application keys are in its deletion set.
- Closing planning handles does not revoke committed jobs. Reading after expiry,
  missing chunks, eviction, revocation or integrity failure gives a distinct
  error; never silently rerun SQL. Concurrent DDL invalidates unpublished
  dependency resolution; already committed final bytes remain readable.
- Limits: proposed one-hour TTL, 1 GiB retained output/job, 256 MiB coordinator
  working budget and explicit tenant total quota. Meter network/storage before
  publication; fail closed when quota cannot be reserved. Upload can double
  network transfer and result storage; Redis replication multiplies storage.
  No implicit paid infrastructure provisioning is authorized by this design.

Required prototype tests: concurrent insert/update/delete plus DDL during
materialization; identical partial/full retries after writes and planner close;
expired/revoked/evicted/corrupt batches; publication failure and coordinator death
before/after manifest commit; worker death; cancellation/early release; independent
credentials; cross-slot routing; orphan cleanup and quota races. Only the
existing live-read consistency contract is implemented/tested in this change.
