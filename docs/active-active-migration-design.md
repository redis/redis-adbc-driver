# Automatic migration to Active-Active: proposed design

Status: design proposal, 2026-10-22. No migration command, numeric regional
allocator or automatic cutover described here is implemented or qualified.
Refs [#491](https://github.com/alberttwong/redis-adbc-driver/issues/491) and the
[change contract](sdlc/changes/active-active-numeric-migration/spec.md).

The product requirement is an opinionated, automatic transition from an ordinary
Redis deployment to Active-Active. Applications should retain their schemas,
values, physical row keys, numeric `__rowid`, INSERT/SELECT code and Arrow types.
Operators select the target deployment and run one resumable migration workflow.
They do not assign region numbers, rewrite application IDs, export/re-ingest rows
by hand, create regional indexes manually or implement allocator recovery.

Automatic means the controller executes and verifies a transition whose
prerequisites are satisfied. It does not mean an unannounced topology switch,
zero downtime, global transactions, or bypassing an incompatible workload.

## Required deployment architecture

The target is **Redis Software with verified 4x QPF in an Active-Active
topology**. Begin with an ordinary Redis Software RAM database with Search and
verified 4x QPF, then migrate to a CRDB spanning at least two independent Redis
Software clusters/sites. Each participating database instance must have active
4x QPF and verified effective Search configuration. Two shards or local replicas
inside one cluster are not a multi-instance Active-Active qualification.

ADBC clients use the nearby site's Software proxy. Row writes and Search queries
execute locally; Redis Software supplies cross-site CRDT replication. The
controller discovers CRDB membership and the corresponding local database
instances through Software management interfaces. Do not require Redis Cloud
subscriptions, Cloud region APIs or Cloud API credentials for this architecture.
Ordinary Cluster API connectivity is a separate driver capability, not the
required connection path for these Software proxy deployments.

Verify QPF on every participating instance through management readback before
functional or driver-performance tests, and after changes that can alter it.
Record active scaling factor 4, effective Search workers, routing/connections,
shard count, server/Search versions, resource allocation and CRDB membership.
The current repository Software fixture checks 6 Search workers, `mnp` routing
and 32 connections; validate the expected mapping for the qualified version.
Refuse qualification if any instance cannot meet the target. A setting in a
creation request is not effective verification, and 4x QPF is not a promise of
4x end-to-end ADBC throughput.

This exact Software/4x/Active-Active combination still needs live qualification.
Earlier Cloud Pro experiments and a single Software/4x fixture cannot establish
it. Redis Cloud is historical evidence only for this proposal, not an alternative
acceptance target or a substitute when Software qualification fails.

## One storage contract

Use the same versioned managed-table format on ordinary and Active-Active
deployments: authoritative HASHes, stable owned prefixes, numeric `__rowid`,
exact Arrow conversion and explicit index definitions. Keep metadata in the
existing `adbc:{meta}:` hash slot. RESP3 is required; TLS verification remains on.
Client routing is separate from identity: ordinary Cluster API routing remains
supported; an Active-Active regional proxy uses a single-endpoint client.

Change the driver's default for new managed databases: the common format and
allocator interface apply from their first write, even on an ordinary cluster.
There is no separate migration-ready profile that users must anticipate enabling.
Version this format explicitly and ship it as a breaking change. Do not build a
legacy adapter, dual-format read path or in-place conversion for the old driver
layout. Automatic migration starts with an ordinary database initialized in the
new common format. Reject older/mixed formats before modifying either database.
Reworking allocation, metadata, index lifecycle and recovery is in scope;
preserving the current implementation is not a requirement.

For this common format, store missing-value/default semantics and allocation
provenance without assuming that one global scalar counter orders every write.
Ordinary mode may optimize its implementation for one writer region, but it must
exercise the same format, allocator interface and recovery state machine. Keep
topology-specific capability checks explicit rather than maintaining unrelated
ordinary and Active-Active storage engines.

The abandoned opaque-key prototype is not the proposed migration format. This
proposal retains numeric identities in both topologies.

## Numeric IDs and region ownership

Reserve disjoint, immutable numeric intervals per table and writer incarnation.
An incarnation belongs to one verified regional instance; a region replacement
or uncertain recovery receives a new incarnation and unused interval. Different
processes in that region receive disjoint allocation blocks within its interval.
Region identity is management-derived, not an application-supplied integer or a
truncated hostname hash. Never reuse an issued interval or block.

The common allocator returns positive IDs strictly below `2^53`, matching the
current partition boundary and exact Search numeric representation. IDs are
identities, not timestamps; gaps are valid and cross-region allocation has no
global insertion order. Explicit `ORDER BY __rowid` remains numeric ordering.

An ordinary database already records every issued interval/block in this ledger.
Migration preserves those reservations and existing IDs, then assigns unused
intervals to additional regional incarnations. For example, an ordinary region
with reserved IDs 1–1000 keeps that reservation while two new incarnations receive
1001–2000 and 2001–3000. These illustrative interval sizes are not a public
capacity policy. Renewals may use any unused interval; no fixed region bit field
forces renumbering. Never reconstruct reservations from only the largest visible
row: deleted rows and allocations that were never written still consume IDs.

Preflight calculates required capacity and headroom. Reject automatic migration
if IDs, counters or required reservations exceed the exact numeric range, if
metadata is corrupt, or if a physical key and stored row identity disagree.
Never wrap, truncate, coerce or reassign an existing ID to make migration fit.

### Allocation authority and recovery

Use one serialized, durable allocation ledger outside the asynchronously
replicated Active-Active namespace. The ledger records table identity, database
lineage, region incarnation, interval/block reservations and migration state.
Reservations commit durably before a block is returned. A retry with the same
request identity returns the same reservation; another request cannot receive
that block. A crash burns unused IDs rather than reclaiming them.

The driver consumes a block in memory under process-local synchronization. On
restart it obtains a fresh block; it does not reconstruct a usable block from a
regional Redis counter or reuse a partly consumed cached block. Persisting a
replicated `INCRBY` result or `SET NX` marker is not a substitute for this ledger.
This avoids reissuing identities when Redis data is restored behind its previous
allocation state. The same block/identity abstraction applies to ordinary mode;
there is no legacy ordinary counter adapter.

This is a deliberate control-plane dependency. Existing leased blocks may be
consumed while the ledger is unavailable; new reservations and migration stop.
Reads continue. There is no fallback to random keys, local CRDT counters or a
different numeric scheme. The ledger's supported durable backend, deployment and
backup/fencing mechanism must be selected and validated before implementation is
accepted. A Redis proxy alone does not supply this stronger durability contract.
Automatic provisioning must describe its cost and require the same resource
authorization as the target deployment; no extra service is provisioned by this
design document.

Bindings include database lineage and a region incarnation. Restores and clones
must enroll before writes resume, obtain new allocation blocks and retain all
historical reservations. Do not infer safe recovery from a healthy PING, a
missing counter or the highest row visible in one region. Recovery fencing must
also prevent an old controller or old driver from re-enabling retired writers.

## Automatic, resumable cutover

The following names describe states, not an existing CLI or API.

1. **Inspect.** Discover source/target topology through Software management,
   verify RAM, Search, supported versions, TLS, no eviction, persistence,
   ownership and ACLs. Verify active/effective 4x QPF on the ordinary source and
   every participating Active-Active instance; validate CRDB membership.
   Require the common format and reject older or mixed formats. Inventory
   tables, IDs, indexes, views, constraints, defaults, pending recovery
   and retained descriptors. Produce a concrete compatibility/capacity report.
   This phase is read-only and does not modify an ordinary database.
2. **Prepare.** Enroll the ledger and immutable database lineage, bind regional
   identities and choose a DDL owner automatically from the source region.
   Stage target metadata/index definitions in an isolated target. Verify every
   writer uses a migration-capable driver and can be fenced. Credentials and
   operational identifiers stay in private state, not SQL or published reports.
3. **Fence and drain.** Block new source writes using enforceable credentials or
   routing controls, drain acknowledged and uncertain operations, and complete
   ordinary cleanup/recovery. A driver marker alone cannot fence old versions
   or raw Redis writers. Wait for every required writer and reader boundary;
   stop if a client cannot be fenced. Record the write interruption explicitly.
4. **Transfer and reserve.** Capture a consistent source baseline while writes
   are fenced. Use a supported Software transfer when qualified, otherwise copy
   authoritative HASHes and metadata preserving keys and IDs. Ordinary Arrow
   INSERT/ingestion must not regenerate physical IDs. Retain all issued source
   reservations and reserve new regional intervals durably; never reset allocation.
5. **Reconcile and verify.** Create compatible regional indexes with backfill,
   wait for readiness and replication, and verify complete schemas, typed values,
   identities and row membership against the fenced source. Counts alone are
   insufficient. Validate metadata and allocator bindings in every region.
   Never issue a destructive index replacement against application data.
6. **Commit.** Persist the new generation and compatibility manifest, switch
   connection profiles, and enable target writers only after all required
   checks pass. The driver loads region bindings from the manifest; applications
   do not embed region codes. A configured stable connection profile/resolver
   can hide endpoint changes; existing direct URI users must update their URI.
7. **Observe.** Keep the source fenced for the agreed rollback window and emit a
   private completion report. Do not automatically delete the source, backups or
   Software resources. Reopening the old source after target writes is not rollback:
   those writes first need an explicitly verified reverse migration.

Every transition is journaled durably and is idempotent. Only one controller may
advance the operation; fencing survives controller replacement. A crash before
commit resumes verification or aborts while the source is still authoritative.
After commit, it resumes the target generation; it never silently enables both
old and new writers. Block issuance may leave gaps on a retry, never duplicate
IDs. A partially provisioned target remains isolated from application writes.

## Preserve behavior rather than silently weaken it

Identity alignment does not solve distributed constraints or schema conflicts.
The first automatic migration target is an append/read workload. Preflight must
reject incompatible persisted features such as PRIMARY KEY/CHECK enforcement,
mutable schema workflows, temporary/adopted tables and recovery in progress
until equivalent semantics have been designed and qualified. It cannot prove
future SQL usage from metadata; deployment policy must declare append/read and
the driver must enforce it after cutover. Unsupported statements return errors.
Such applications are not advertised as requiring zero code changes.

Preserve supported defaults, NOT NULL, exact values and all-NULL rows. Never
silently drop a constraint, convert a default to a constant or change an Arrow
type. Migration must materialize ordinary lazy defaults into equivalent HASH
values, or retain their exact interpretation before regional writes begin. The
new ordinary format must use the same representation from its first write;
conversion of old-format lazy defaults is outside scope.
Preserving physical IDs does not preserve the meaning of `id <= high_water`
across regions. Global numeric watermarks cannot implement missing-column
defaults, constraint rechecks or snapshot partitions after cutover.

Keep the logical index schema and its precision-safe pushdown rules where
qualified. Reconcile each regional physical index from versioned metadata.
Do not unindex every SQL column as an incidental migration step; any unavoidable
index change must be explicit in preflight and pass exact-result checks.
Mutable tag/NaN proof metadata also needs a regional-safe replacement before its
optimization is enabled. Correct fallback is preferable to incorrect pushdown.

Finish or explicitly invalidate active cursors/partition readers at the boundary;
never reinterpret an existing descriptor against a different endpoint or format.
ADBC partitions remain excluded from the initial Active-Active target. Future
support requires per-region bounds or captured membership, not a single counter.

Regional reads remain asynchronous live reads. Numeric allocation supplies
identity uniqueness under the ledger contract, not a global snapshot,
cross-region uniqueness for application columns, exactly-once ingestion or
read-your-writes after failover. Retry of an uncertain INSERT may create a second
row with a new ID; preserving ordinary retry behavior does not eliminate it.

## Acceptance before shipping

### Performance and Redis feature use

Performance means the ADBC driver's end-to-end performance as observed by its
caller. Primary measures are SQL INSERT/Arrow ingestion rows and bytes per second,
query time to first Arrow batch and complete fetch, p50/p95/p99 ADBC operation
latency, and driver CPU, allocations and peak memory. Include planning, allocation,
Redis I/O, decoding, Arrow conversion and delivery through the public ADBC
boundary. Ingestion timing ends at the driver's completed write response; query
timing ends only after consuming the complete Arrow stream and validating its
schema/values. Neither server command time nor query submission alone establishes
driver performance. Measure native ADBC and Python binding paths separately.

This proposal is not a measured optimum. The extra allocation authority is a
correctness/operations tradeoff and must not become a per-row WAN dependency.
Treat these requirements as acceptance gates alongside migration correctness:

- Keep normal reads and row writes regional. Reserve numeric blocks in batches,
  prefetch before exhaustion with bounded memory and concurrency, and consume
  them locally. Report cold-start/refill latency separately from steady-state
  latency. During authority failure, consume already committed blocks only;
  availability is bounded by the remaining reservations, not unlimited.
- Use persistent go-redis pools, bounded non-transactional HASH pipelines and
  projected HASH fetches. Do not create connections, query management APIs,
  reserve IDs or validate every index once per row. Keep one consistent
  allocator API without imposing one identical network path on every topology.
- Preserve safe Search filtering, sorting, aggregation and cursor pushdown.
  Use NUMERIC/TAG/SORTABLE only where they preserve SQL values and NULL behavior;
  measure their indexing/replication/memory cost. Keep regional index schemas
  consistent. The earlier sentinel-only/full-HASH-scan prototype is not the
  performance target. Precision-sensitive residual evaluation remains required.
- Reconcile indexes at provisioning, generation changes and explicit recovery.
  Cache verified immutable definitions within a bounded lifetime; refresh on
  missing-index/schema errors. Do not add FT.INFO/backfill readiness polling to
  every successful query. A schema change invalidates its generation's cache;
  query retries must preserve existing uncertain-write rules.
- Distribute row keys across shards; retain the shared hash tag for metadata
  operations that actually require one slot. Avoid adding a global counter,
  lock or metadata write per appended row. Do not mistake CRDT convergence for
  global transaction/constraint coordination.
- Verify Software 4x QPF and effective Search workers/routing on every participating
  Active-Active instance for tests. Features that do not
  benefit this HASH/SQL/Arrow workload are not adopted merely to use more Redis
  products; compatibility and exactness remain requirements.

Before selecting defaults, compare the proposed driver's ordinary mode with the
current ordinary ADBC driver on identical data, SQL, Arrow types, batch sizes,
concurrency, client hardware and server settings. Compare ordinary and
Active-Active modes using matched regional resources and disclose topology and
replication differences. Include selective reads, scans, sorts and aggregates,
small and bulk writes, multiple shards/regions, cold and warm connections/metadata,
and healthy and impaired ledger/WAN conditions. Report allocation/refill latency,
pool/pipeline saturation, rows examined, index amplification and replication lag
as diagnostic explanations of the end-to-end ADBC results. Control-plane cost is
an operational tradeoff, not the definition of driver performance. A native
pipelined Redis baseline can isolate driver overhead but cannot replace ADBC
acceptance measurements. Declare regression budgets before measurements and
record actual results; do not describe the architecture as optimal based on the
design alone. No ClickBench run is part of this proposal.

The performance direction follows Redis's
[connection pooling guidance](https://redis.io/docs/latest/develop/clients/pools-and-muxing/)
and [pipelining guidance](https://redis.io/docs/latest/develop/use/pipelining/).
Regional provisioning accounts for Redis's documented
[non-replicated Active-Active indexes](https://redis.io/faq/doc/1pr4eqrt7r/are-indexes-replicated-between-the-replicas-of-an-active-active-database).

### Migration and failure qualification

Test ordinary databases created in the new common format through the workflow.
Reject legacy and mixed formats without mutation. Preserve every key, ID, schema
and typed value; include allocation gaps,
large IDs, decimals, binary/NUL values, all-NULL rows and lazy defaults. Exercise
multiple ordinary shards and regional concurrent writes, block exhaustion,
controller/client crashes at each transition, duplicate requests, stale clients,
ledger unavailability, region loss, restore behind an allocation reservation,
clone enrollment and interrupted/resumed migration. Prove no ID reuse or unsafe
source reopening. Test negative preflight without source changes.

Both the ordinary source and the multi-site Software Active-Active target require
active, verified 4x QPF. Historical Cloud Pro results are not acceptance evidence
for this target. No
Valkey, ElastiCache or Memorystore support is part of this proposal. Record actual
skips, deployment cost/ownership and verified cleanup privately and sanitize
public evidence. Existing opaque-key prototype results are not numeric allocator
or migration acceptance evidence.

## Source basis and decisions still required

The current driver uses `allocRows`/`writeRows` in `go/store.go`, scalar
`lastRowID` and `MissingThrough` in `go/alter.go`, and numeric range boundaries in
`go/partitions.go`. These are migration-sensitive assumptions, not just an ID
generation helper. Implementation must replace their topology assumptions while
retaining ordinary behavior.

Redis documents [semantic counter convergence](https://redis.io/docs/latest/operate/rc/databases/active-active/develop/data-types/strings/),
which does not establish unique per-region allocation results. Its
[application failover guidance](https://redis.io/docs/latest/operate/rc/databases/active-active/develop/app-failover-active-active/)
describes asynchronous replication and application-managed endpoint failover.
The durable reservation ledger and fenced migration workflow above are proposed
driver architecture, not capabilities asserted by those documents.

Before runtime work, select the ledger backend and its fencing protocol, qualify
Software transfer/restore and site-identity APIs, and define the stable
connection profile contract. Until those decisions and the failure matrix pass,
the implementation must not claim automatic migration or seamless failover.
