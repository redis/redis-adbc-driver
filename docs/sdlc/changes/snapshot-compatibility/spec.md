# Specification: snapshot compatibility

Date: 2026-11-02. Source: 7c1a7ca. Status: implemented and locally validated; draft PR pending human review.
See [intent](intent.md), [plan](plan.md), and PR #284.

ALTER stages a new immutable physical layout and atomically publishes it. Existing
pins retain old columns and values. Conversion or constraint failures publish
nothing. Rename can reuse immutable rows when the physical layout is unchanged.
DDL rewrites retain existing per-statement resource limits.

Temporary schemas remain private to their connection. Their descriptors and data
references are included in manifests, including when other connections publish.
Liveness is global, outside immutable workspaces. Close or abandoned-session sweep
publishes descriptor removal without directly destroying pinned physical storage.
Failed statements must not leave phantom local temporary names.

The user selected managed copies with explicit refreshes for external data.
AdoptHashes Apply creates or refreshes a private managed table. Only the same
source origin may replace an existing import; unrelated tables are protected.
The source prefix, HASHes and application indexes are never owned or deleted.
Foreign-index enable imports missing managed copies in redis_foreign; explicit
foreign_indexes_refresh=true or RefreshForeignIndexes updates each copy and
removes copies of missing source indexes, with one publication per index.
Copies in redis_foreign remain read-only. Sources overlapping driver control
storage are rejected. Capture observes external data over a scan and is not an
atomic transaction with external writers. Published copies have atomic reads.
Inspection pins metadata. Sweep delegates to snapshot reference-aware reclamation
and reports no legacy released-prefix row counts.

Live partition production pins planning metadata. Each ReadPartition pins its
own generation through stream consumption; the set is not one shared snapshot.
A stable random table lineage survives deltas/compaction, so ordinary writes do
not invalidate descriptors. TRUNCATE, replacement and ALTER layouts renew the
lineage. Scans seek exact row-ID directories and resolve overlays before filters.
Retained production captures managed rows from one coordinator pin and publishes
independent Arrow results; old partition shape/quota/deployment limits remain.
DROP PARTITION RESULT revokes output control records outside managed manifests.

Protocol version 5 rejects earlier drafts 1–4: their writers do not preserve
references for temporary and imported objects. There is no migration path.
Tests check result values, ownership, failures, cancellation and pinned lifetimes.
