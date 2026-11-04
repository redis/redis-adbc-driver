# Plan: snapshot feature and performance parity

Date: 2026-11-02. Status: in progress. See [intent](intent.md), [spec](spec.md).

| Workstream | Status |
|---|---|
| Shared-generation live partition capabilities, release and expiry | Implemented; proxy/native checks passed |
| Visibility-correct Search predicates and parallel reads | Implemented; targeted and race checks passed |
| Exact index aggregation/grouping/top-K on segmented visibility | Implemented with bounded masks and exact fallbacks; seven-case read comparison recorded |
| Incremental metadata, independent publication and index lifecycle | Lazy validation and one-build index lifecycle/no-op publication implemented; pre-execution admission and metadata sharing/GC implemented; incremental row references verified locally; independent publication verified locally; bounded bucket construction verified locally |
| Large atomic writes/ingestion and sustained compaction | Durable dirty-table scheduling and GC batch progress verified locally; bounded SQL/Arrow row images and ALTER/rekey with durable outcomes implemented; process/input resume, remaining utility quotas and broader sustained throughput work remain open |
| Full feature/native/platform/Cluster API validation | Current protocol-14 Cluster Go, primary promotion, all-node AOF/RDB restart and replicated proxy race/native checks verified; hosted CI and full feature/native/platform matrix remain open |
| Comparative benchmarks, review, PR evidence and owned cleanup | Seven-case read comparison and self-review recorded; remaining benchmarks/cleanup pending |

Implement and verify coherent milestones; keep PR #284 reviewable. Provision
only owned Redis Software fixtures with verified 4x QPF. Keep licenses,
credentials and operational identifiers private. Do not infer success from unit
integration skips or incomplete fetches. Record all remaining work explicitly.

The [consistency-boundary recommendation](consistency-boundary.md) proposes a
shared driver consistency layer across execution paths. A new
Redis server extension is not required by this plan.

The [read checkpoint evidence](validation.md), [raw timing samples](read-benchmark.json)
and [author review](review.md) cover `9bc80c0`. They do not close the remaining
metadata, write/compaction, full feature/platform and benchmark requirements.

The [index lifecycle checkpoint](index-validation.md) records one final build per
private output and zero builds/publications for no-op UPDATE/DELETE, with full
snapshot race and native checks. It does not close the remaining parity work.

The [admission checkpoint](admission-validation.md) verifies gate/full-lineage
waiting before SQL or Arrow execution, cancellation/pin cleanup, one-time input
consumption and atomic IPC late-error behavior. Remaining CI and performance
gaps stay in the active plan.

The [metadata sharing checkpoint](metadata-validation.md) removes unrelated native
value copies and legacy name registries, with independent metadata reclamation.
The [next reference/publication design](row-reference-design.md) records remaining
catalog enumeration, row-reference and independent-writer work.

The [descriptor-owned row-reference checkpoint](row-reference-validation.md)
removes publication descriptor enumeration and aggregate base-reference copies;
independent publication is covered by the [publication checkpoint](publication-validation.md);
bounded bucket construction verified locally.

The [bounded bucket checkpoint](bucket-validation.md) verifies point metadata
reads, ordered field directories, resumable construction and reference-safe
interruption/expiry. Large operations and sustained compaction remain open.

The [compaction queue checkpoint](compaction-queue-validation.md) removes idle
catalog descriptor discovery and verifies conditional queue ownership, prefix
admission, lease recovery and two-table writes without caller retries.

The [Ray compatibility checkpoint](ray-validation.md) verifies shared-generation
worker replay after DML/TRUNCATE/DROP, fresh-plan visibility, retries across an
intervening write and TLS rejection without inherited CA trust: 17 cases passed.
The remaining full feature/platform checks stay open.

The [wide-fetch checkpoint](wide-validation.md) restores positional HASH/Arrow
reads through exact snapshot visibility and preserves the complete wide test's
optimized/fallback, value and lifecycle assertions. Local comparative timings
show selective improvement and approximately unchanged full scans; this does
not close the remaining full feature/platform or performance requirements.

The [managed-import report and ACL checkpoint](import-report-validation.md)
restores complete dry-run/Apply/refresh reports and verifies CLI ownership and
explicit refresh. A Software API principal probe confirms that protocol 12's
`adbc:*` row layout defeats the legacy schema ACL recipe. The
[protocol-13 namespace checkpoint](schema-namespace-validation.md) repairs that
HASH/Search access boundary and makes the expanded Software gate pass; complete
restricted-driver command/pin recipes remain open. The
[source-key checkpoint](source-key-validation.md) restores imported `__key`
identity, including generic/positional/late projections and pinned partitions.
Remaining adoption/sweep/ACL regression families and the broader parity work
stay open.


The [import-failure checkpoint](import-failure-validation.md) reproduces and fixes
lost reports after late private-index failure. Atomic rollback, private-index
reclamation and source ownership are checked for create and refresh. Legacy
adoption/unfinished reservations are rejected without migration; current large
operation resumption and the remaining compatibility families stay open.


The [import-index checkpoint](import-index-validation.md) separates source
membership from private native index policy for HASH/foreign copies. Converted
text values and renamed columns can be indexed; explicit capture indexes and
private IndexColumns can be combined. Existing SQL copies remain independent of
source-index DROP/change; broader performance and compatibility work stays open.


The [managed write checkpoint](import-write-validation.md) preserves import
origin through Arrow replacement and binds provenance through joined DML and
explicit derived/CTE projections. SQL/Arrow private-write compatibility and raw
keyed collision coverage are retained; sweep/ACL, large operations, sustained
performance and full platform/Cluster validation remain open.


The [sweep checkpoint](sweep-validation.md) replaces the empty snapshot sweep
result with non-destructive eligibility preview, bounded reference-aware draining,
confirmed counts and partial errors/resumption. Protocol/CLI lifecycle checks
protect pins, active writers and unregistered objects. Restricted-schema ACL sweep
behavior, the full permission recipes and the broader parity work remain open.

The [durable GC batch checkpoint](gc-batch-validation.md) atomically consumes row
directory entries with their zero-reference row deletions. Fresh collectors keep
progress after interrupted replies without SCAN cursors or replay. It supersedes
the sweep checkpoint's directory-rescan limitation; broader large-operation,
platform and comparative performance requirements remain open.

The [insert batch checkpoint](write-batch-validation.md) stages SQL/Arrow inserts
in bounded atomic row batches with durable outcomes, resolves lost replies by
reading rather than resending, and slices Arrow conversion. It removes the old
cumulative insert ceiling while preserving final publication. Unread-input/process
resume, generic large rewrites, sustained compaction and the full matrix remain
required; this is not completion of the large-operation workstream.

The [rewrite batch checkpoint](rewrite-batch-validation.md) extends bounded
row images to UPDATE replacements, DELETE tombstones and ALTER/rekey copies,
with private-only replacement admission, NULL/provenance preservation and
independent no-op accounting. It supersedes the prior generic UPDATE/DDL row
ceiling limitation; operation/input resume and broader parity work remain open.

The [current Cluster checkpoint](cluster14-validation.md) reruns multi-primary
connectivity, managed copies, partitions and node fences on protocol 14, adding
bounded row-batch outcomes and private-page/shared-metadata recovery assertions.
Promotion and abrupt all-node AOF restart preserve exact pinned/committed/private
state and resume compaction; this does not close RDB or the full platform matrix.

The [RDB checkpoint](rdb-validation.md) separately verifies fresh completed RDBs
with AOF disabled on every shard, then recovers routed sentinels, committed/private
state and compaction across an abrupt three-node restart. The interrupted stream
fails explicitly without replay. This closes that RDB restart evidence gap, not
portable backup import, uninterrupted streaming or the full platform/performance
requirements.

The [import journal checkpoint](import-journal-validation.md) removes the
whole-import retained-capture and Apply candidate ceilings through private
reference-owned capture pages. Input is frozen before destination HASHes are
produced and consumed after acknowledged output. This is groundwork for large
operation progress, not a process-resume interface. The measured 10,000-row
refresh median regressed by 4.9%; import throughput and durable operation/input
resume remain open.

The [remaining import resume design](import-resume-design.md) specifies the
durable plan/input-complete transition, independent job references, exclusive
takeover lease, metadata/index restoration and explicit client continuation still
required. Atomic output/capture consumption is a prerequisite; no public resume
API or process-recovery claim follows from the private journal alone.

The [atomic import output checkpoint](import-output-validation.md) links each
registered output batch, capture consumption and durable outcome in one script.
Incomplete runtime outcomes fail closed. This removes an ambiguous continuation
boundary while the durable job/lease/plan work above remains required.
