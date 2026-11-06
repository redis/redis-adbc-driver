# Minimal atomic statement snapshot acceptance

Status: user-requested scope for PR #284, 2026-11-05. This supersedes the broader
[parity specification](spec.md) as this PR's completion boundary. Deferred work
is not being claimed as implemented. See [delivery boundaries](delivery-scope.md).

## Required behavior

1. A managed-table statement acquires one committed generation before planning
   and reads that generation through the complete result/Arrow stream. Concurrent
   writes cannot mix membership, values or schema from different generations.
   An unavailable/expired view fails explicitly rather than switching to live data.
2. An autocommit write stages private changes and publishes once. Other statements
   observe the complete old or complete new state. Errors before publication leave
   committed data unchanged; unknown outcomes never trigger SQL/input replay.
3. Readers retain the data, metadata and indexes they need through concurrent
   writes, replacement, DDL, DROP and maintenance. Pins, cancellation and Close
   have bounded cleanup; reclamation cannot delete objects still referenced.
4. Existing execution paths, including already implemented optimized reads and
   shared-generation live partitions, preserve the same visibility and lifetime
   contract. Use existing safe fallbacks where necessary. No new fast path,
   partition mode, SQL feature or compatibility expansion is required.
5. Supported Cluster API routing, failover and configured AOF/RDB recovery must
   retain acknowledged committed state or produce the documented explicit failure;
   no mixed/corrupt snapshot may be presented. Managed data remains in its existing
   single-slot layout; this is not a coordinated distributed snapshot claim.
6. Preserve existing authorization/native client contracts and disclose storage,
   retention and throughput limits. Record source-specific regression/native and
   supported platform evidence. Keep managed external copies plus explicit refresh,
   fresh protocol-16 storage with no migration, autocommit-only scope, Active-Active
   exclusion and noeviction. No new option disables snapshot correctness.

## Completion evidence and remaining work

The [protocol-16 Cluster report](cluster16-validation.md),
[recovery cleanup report](pin-cleanup-recovery-validation.md), existing visibility/
lifecycle regressions and native validation contain the source-specific evidence.
The later [publication-integrity guard](publication-integrity-validation.md)
has its own current-source local evidence and terminal green hosted matrix.
The later reference/checkpoint diagnostics are test-only, with separate evidence. The older hosted
checkpoint below does not prove that subsequent production change.

At production revision `272bbd4d`, hosted workflows are all terminal SUCCESS:
[Test](https://github.com/redis/redis-adbc-driver/actions/runs/42172244144),
[Release](https://github.com/redis/redis-adbc-driver/actions/runs/42172244104),
[DuckDB](https://github.com/redis/redis-adbc-driver/actions/runs/42172244094).
Test includes both AMD64 and ARM64 Software jobs; proxy workflow success does
not replace the separate actual Cluster API/fault/persistence evidence.

A fresh exact-production `272bbd4d` RDB diagnostic passes in **47.38 seconds**:
all three physical-primary sentinels recover, interrupted streaming returns an
explicit error without replay, established/fresh clients read exact values,
private rows/outcome remain isolated, and durable compaction completes. The
checkpoint-only subcase passes in 0.22 seconds. A private readiness preflight
first timed out because it inspected effective QPF from an API response without
requesting those fields; correcting that query admitted the verified run. This
is a preflight correction, not a driver fix or an extra passing recovery run.

On 2026-11-06, the user approved tracking the retained post-promotion RDB
metadata error separately in [#345](https://github.com/redis/redis-adbc-driver/issues/345),
rather than requiring its causal explanation to complete #344 / PR #284.
The observed query returned an explicit I/O error; no mixed or incorrect result
was observed. The cause and durable-metadata condition remain unknown. Passing
reruns do not establish a fix, a fluke or a Redis server defect.

The correctness gate remains: unavailable/inconsistent views fail explicitly,
with no live-data fallback, mixed/incorrect rows or uncertain SQL/input replay.
Evidence of lost acknowledged state or deletion of referenced metadata/data
also remains a core blocker. Splitting the investigation does not prove those
properties safe or excuse such a defect. Preserve all recovery tests, assertions,
failed evidence and the diagnostic observer. Final source-specific checks,
maintainer review and owned-resource cleanup remain required.

## Explicitly deferred

The [deferred-work index](deferred-work.md) links seven labeled follow-up issues
and the separately saved WIP code, with exact validation boundaries.

Public operation/input resume and process takeover; performance parity with the
mutable driver; additional metadata/index optimizations and new read fast paths;
new sustained-throughput targets; coordinated cross-shard snapshots; additional
compatibility/features. Their upstream requests and existing measured gaps stay
visible in the delivery record. Existing correctness-preserving maintenance is
still necessary; no throughput guarantee or upstream delivery date is assumed.

The private import-capture/reserved-row-ID development branch is outside this
core PR. Do not merge it merely to satisfy the earlier, broader parity plan.
