# Plan: snapshot feature and performance parity

Date: 2026-11-02. Status: in progress. See [intent](intent.md), [spec](spec.md).

| Workstream | Status |
|---|---|
| Shared-generation live partition capabilities, release and expiry | Implemented; proxy/native checks passed |
| Visibility-correct Search predicates and parallel reads | Implemented; targeted and race checks passed |
| Exact index aggregation/grouping/top-K on segmented visibility | Implemented with bounded masks and exact fallbacks; seven-case read comparison recorded |
| Incremental metadata, independent publication and index lifecycle | Lazy validation and one-build index lifecycle/no-op publication implemented; pre-execution compaction admission implemented; metadata sharing/publication pending |
| Large atomic writes/ingestion and sustained compaction | Pending |
| Full feature/native/platform/Cluster API validation | Proxy race/native milestone checked; full feature/platform/Cluster reruns pending |
| Comparative benchmarks, review, PR evidence and owned cleanup | Seven-case read comparison and self-review recorded; remaining benchmarks/cleanup pending |

Implement and verify coherent milestones; keep PR #284 reviewable. Provision
only owned Redis Software fixtures with verified 4x QPF. Keep licenses,
credentials and operational identifiers private. Do not infer success from unit
integration skips or incomplete fetches. Record all remaining work explicitly.

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
