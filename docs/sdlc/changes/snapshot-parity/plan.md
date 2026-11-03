# Plan: snapshot feature and performance parity

Date: 2026-11-02. Status: in progress. See [intent](intent.md), [spec](spec.md).

| Workstream | Status |
|---|---|
| Shared-generation live partition capabilities, release and expiry | Implemented; proxy/native checks passed |
| Visibility-correct Search predicates and parallel reads | Implemented; targeted and race checks passed |
| Exact index aggregation/grouping/top-K on segmented visibility | Implemented with bounded masks and exact fallbacks; seven-case read comparison recorded |
| Incremental metadata, independent publication and index lifecycle | Lazy validation and one-build index lifecycle/no-op publication implemented; pre-execution admission and metadata sharing/GC implemented; incremental row references verified locally; independent publication verified locally; bounded bucket construction verified locally |
| Large atomic writes/ingestion and sustained compaction | Durable dirty-table scheduling verified locally; large atomic operations and broader throughput work remain open |
| Full feature/native/platform/Cluster API validation | Proxy race/native milestone checked; full feature/platform/Cluster reruns pending |
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
Native wide fetch and the remaining full feature/platform checks stay open.
