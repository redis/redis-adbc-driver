# Multi-shard managed ingestion assessment

Date: 2026-11-20. Source: `1b5918e6`. Status: investigation, not implemented or approved.
[Intent](intent.md) · [Specification](spec.md) · [Plan](plan.md)

## Evidence and upstream status

`go/snapshot.go` defines protocol 17 and places the catalog and metadata under
`adbc:{meta}:snapshot:17:`. `snapshot_rows.go` constructs managed row prefixes
with `{meta}`; index names, exact row directories and atomic copy/write scripts
rely on that placement. `snapshot_append.go` coordinates private append intents
and publication into the same logical table. Changing only row prefixes breaks
fences that atomically reference catalog, ownership graph, directory and rows.

Redis's [Cluster specification](https://redis.io/docs/latest/operate/oss_and_stack/reference/cluster-spec/)
limits multi-key operations to one slot. Hash tags deliberately co-locate keys.
[Pipelining](https://redis.io/docs/latest/develop/using-commands/pipelining/)
reduces request overhead; it does not make one slot's command execution
parallel across primaries. QPF increases Search query resources; it is not a
row-placement protocol. Driver fixtures verify QPF separately from placement.

As checked through the primary GitHub API on 2026-11-20, Redis requests
[#16180](https://github.com/redis/redis/issues/16180) (reusable read views) and
[#16184](https://github.com/redis/redis/issues/16184) (coordinated cross-shard
views) are OPEN. Their request bodies do not establish shipped APIs, Search
participation, product availability or a schedule. This inspection is not proof
that every product lacks another suitable primitive; product confirmation is
an explicit discovery gate. Driver [#339](https://github.com/redis/redis-adbc-driver/issues/339)
tracks native views and [#340](https://github.com/redis/redis-adbc-driver/issues/340)
tracks coordinated read integration. #568 adds physical ingestion placement;
a read-only view alone does not make multi-shard publication safe.

## Placement and bounded allocation

A proposed new-format table descriptor records a placement version, immutable
logical table identity, generation, partition count, partition-to-tag mapping,
and membership epoch. A deterministic hash of immutable logical row identity
selects a partition. Each partition tag co-locates its row HASHes, exact directory,
local control, durable outcomes and local retention journal. Row identity remains
logical; physical prefixes never become public `__key` or overwrite provenance.

Validate the mapping through the management topology and slot ownership against
physical primaries, including effective QPF on each participant. Several tags
can land on one primary; reject a claimed scaling experiment unless independent
counters show writes on distinct primaries. Partition count is distinct from
writer count. Allocate a bounded worker pool and bounded rows/bytes per partition;
cap both aggregate queued bytes and active workers. Backpressure must propagate
to Arrow consumption. Do not materialize the entire input to repartition it.

Changing partition count or membership is a new immutable placement generation,
not reinterpreting existing IDs in place. In-flight work stays on its captured
epoch or fails explicitly. Redirect handling must not replay uncertain writes.
A supported server contract must define migration/promotion fencing first.

## Publication and ownership: required protocol, unresolved implementation

An immutable generation can be prepared privately on several shards and made
visible through one catalog-root decision. This is a possible *logical* model,
not proof of a complete distributed protocol. Before that decision, every shard
must durably seal exact schema/generation/partition identity, counts, directories,
index readiness and immutable data, and retain its output independently of the
writer process. The central descriptor references only sealed partitions.

A suitable supported facility must supply durable ownership transfer and a
well-defined consistency/durability boundary for these prepared objects and the
catalog decision. A client collecting receipts and CASing the root is insufficient:
GC could release a shard's preparation while the root is being committed, or a
promotion/restore could retain the catalog but lose a partition. Central lease
expiry cannot authorize shard deletion during an unavailable decision read.

Consequently this assessment does **not** propose implementing a home-grown
prepare/commit coordinator or call a central CAS a distributed transaction.
If supported primitives do not supply the needed retention/fencing contract,
implementation is blocked pending design approval and a separately specified
coordinator/recovery protocol. All-or-nothing SQL visibility remains required.

Each local batch must atomically validate local epoch, owner token, unexpired
fence, prefix identity, expected checkpoint and control permissions before
registering directory entries and writing complete images plus its durable
outcome. Use local monotonic fencing generations, not synchronized client clocks.
The central intent records participating partitions and immutable source refs
before work can escape the writer; admission occurs before consuming Arrow.
Exact cross-shard retention/decision proofs remain an upstream/design gate.

## Failure, cancellation, recovery and GC obligations

| Boundary | Required outcome |
| --- | --- |
| One partition refuses before mutation | Stop consumption, cancel siblings; preserve acknowledged counts and prepare ownership until abort is safely decided. |
| Local mutation reply lost | Read that partition's unique durable outcome; never resend unknown mutation or consume source again. If unreadable, report indeterminate and retain discoverable work. |
| Writer/coordinator disconnect | A durable discoverable participant set enables bounded recovery; no input/process resume is implied. |
| Seal partially succeeds | Unsealed generation stays invisible; cleanup proves no publish decision can still refer to each object. |
| Root decision reply lost | Read unique durable decision; unreadable decision remains indeterminate, not assumed abort. |
| Cancel after root commit | Report established publication outcome; cleanup cannot delete committed output because caller context ended. |
| Lease expiry/network partition | Fence stale writers locally; unknown central decision prohibits destructive cleanup. Space/backpressure may increase rather than sacrifice correctness. |
| DDL or competing DML | Validate pinned dependencies/lineage before publication; conflict without replaying SQL/Arrow. |
| Missing partition/failover | Preserve promised boundary or return explicit unavailable/invalidation error; never substitute live rows. |

Reader pins require a supported retention boundary spanning the captured manifest's
partitions. Acquisition must protect all references before exposing rows, bound
partial acquisition and renew/release/expiry, and clean up abandoned participants.
A list of independent shard pins acquired at different times is not automatically
one distributed snapshot. Immutable managed generations simplify value retention,
but do not by themselves solve reference transfer, collection and topology races.

Compaction operates private partition outputs; install must preserve exact source
range/newer tails and transfer source/output refs safely. GC uses registered exact
prefixes, shard-local atomic liveness/fence checks and durable work queues. It
must never scan arbitrary application keys, guess from missing metadata, or treat
a timed-out coordinator as permission to delete. Retention debt and cleanup lag
must be observable and bounded by admission/backpressure.

## Query and schema semantics

One authorized catalog snapshot binds planner, workers and all result pages to
the same schema, placement and generation. Every participating shard must validate
the same identity/epoch; capability or authorization failure fails the statement.
Partition readers resolve newest row versions and tombstones before predicates,
aggregates and LIMIT. Updates preserve original provenance and routing identity.
Duplicate overlay versions cannot become duplicate logical rows at merge.

Search candidate discovery and HASH reads must share the promised boundary.
If Search cannot participate, use an approved exact-directory immutable path
with full predicate checks, not a mixture of live index membership and old rows.
Partition-local aggregates need exact combiners: SUM/COUNT versus AVG state,
NULLs, decimal overflow, NaN, DISTINCT and order-sensitive operations differ.
Top-k/ordered merge needs a global ordering/tie/NULL contract and final
OFFSET/LIMIT; independent shard limits are insufficient in general. Joins,
subqueries and partition-independent execution retain the shared catalog pin.
All canonical queries require complete fetch and independent expected results.

## Storage format, backup and rollout

A physically distributed layout requires a new explicit storage protocol and
capability identifier checked before opening or writing. Protocol 17 remains
single-slot and must be rejected by the distributed reader unless it deliberately
opens the unchanged legacy path. No automatic in-place migration is designed.
A supported explicit export/import would create a new destination and validate
all data before cutover; source ownership is preserved on failure.

Full backups must carry the manifest, exact physical prefixes, placement epoch,
control/ownership/outcome records and every referenced partition at one compatible
boundary. Independent shard RDB files taken at different logical times are not
assumed compatible. Restore must prove complete participant coverage, references,
server/module features, format and effective QPF before queries or writes; reject
partial/incompatible restores and stale in-flight fences. Restore/remap across a
changed shard count requires an explicit supported process, not string rewriting.

## Alternatives and decision

| Option | Benefit | Limitation / disposition |
| --- | --- | --- |
| Keep one slot; bounded batch/index/compaction improvements | Existing publication/backup proof; deployable incremental work (#564–567). | Command-thread ceiling remains; measure improvements, no distributed claim. |
| More clients/readers/pipelines on one slot | Overlap conversion and transport, amortize calls. | Does not distribute row execution. Retain as controlled tuning. |
| Supported coordinated views/bulk or immutable-generation facility | Prefer server-owned failure, membership and lifetime semantics. | Verify exact product/Search/durability contract; open requests are not availability. Preferred discovery path. |
| Driver custom distributed coordinator | Could separate catalog decision from private shard writes. | Substantial new protocol and failure/GC proof; not authorized by this assessment. Deferred design gate. |
| Multiple unrelated tables/databases with client UNION | Easy independent write placement. | Changes table/query/schema/backup semantics; not a substitute for issue acceptance. |
| RDB/offline import | Potential faster bulk-load path where officially supported. | Requires compatible authoritative control/directory/index format and atomic restore; not a live distributed publication primitive. |

Proceed with compatible single-slot improvements now. Keep multi-shard runtime
work blocked on a verified supported coordination contract or an explicitly
reviewed alternative. No multi-primary deployment, distributed ingestion result,
new backup compatibility or all-core speedup is claimed by this design record.
