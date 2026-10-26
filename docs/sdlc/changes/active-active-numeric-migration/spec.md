# Contract: numeric identity and automatic migration

Date: 2026-10-22. Status: design only. Author: Codex; requestor: Albert Wong.
Refs [intent](intent.md), [plan](plan.md), [evidence](evidence.md),
[design](../../../active-active-migration-design.md).

- Required architecture: ordinary Redis Software/4x QPF to a Redis Software CRDB
  across at least two independent clusters/sites, with active 4x QPF and effective
  Search configuration verified separately on each participating instance.
  Clients use local Software proxies; management uses Software APIs. Cloud Pro
  experiments and a single Software instance are not acceptance evidence.
- Ordinary and Active-Active managed tables share numeric `__rowid`, stable HASH
  keys, authoritative values, exact Arrow types and versioned metadata/indexes.
- The common format is the proposed default for all new ordinary managed
  databases. Change existing allocator/default/metadata/recovery architecture as
  needed; do not require applications to opt into migration readiness in advance.
- The release is a deliberate format break. No legacy adapter, dual reads or old
  format conversion. Reject old/mixed formats before mutation. Automatic
  migration applies to ordinary databases initialized in the common format.
- Existing common-format numeric rows and issued reservations remain unchanged;
  use immutable disjoint intervals for regional writer incarnations. IDs remain
  positive and below `2^53`; insufficient capacity is a preflight error.
- Derive regional bindings from verified management topology. Applications never
  allocate region codes. Every returned block is durably reserved in a serialized
  ledger outside the Active-Active namespace; issued ranges are never reused.
- Restart uses a fresh block. Restores/clones enroll with the controller. Ledger
  loss or insufficient recovery evidence blocks new allocation; regional CRDT
  counters and ordinary metadata markers are not recovery authorities.
- One journaled, fenced workflow inspects, prepares, drains, copies preserving
  identities, reconciles indexes, verifies complete data and commits profiles.
  It resumes after interruption and never opens old and new writers together.
- Preserve common-format SQL/defaults/index semantics. Reject unsupported workloads
  or corrupt metadata, never silently drop constraints or regenerate row IDs.
  Lazy defaults and numeric watermarks require explicit conversion/handling.
- Preflight is read-only. Cutover includes a declared write interruption.
  Existing direct URI users change endpoints; a stable profile can hide routing
  changes. Source deletion and post-write reverse migration are separate actions.
- Append/read is the first target; asynchronous regional reads and ordinary
  uncertain-write retry semantics remain explicit. No global snapshot or
  application-column uniqueness guarantee follows from numeric ID allocation.

Acceptance requires the end-to-end and failure matrix in the design. Backend
selection, fencing, Software transfer and profile APIs are implementation gates.
This change adds no runtime support or new compatibility claim.

Performance gates: regional steady-state I/O, batched/prefetched reservations,
pooled pipelined HASH operations, precision-safe Search pushdown, generation-aware
index verification and distributed row keys. Performance is measured at the public
ADBC boundary: ingestion throughput, first-batch and complete-fetch latency,
operation tail latency, driver CPU, allocations and peak memory. Include Arrow
conversion and complete validated consumption; distinguish native and Python
paths. Use matched ordinary-driver baselines and predeclared regression budgets.
Redis timings/lag and control-plane cost are diagnostic/operational measures.
