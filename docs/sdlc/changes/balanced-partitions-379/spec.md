# Specification

2026-10-21; issue #379; baseline `d7346f9`. See [intent](intent.md), [plan](plan.md).

- String statement strategy: rowid default, balanced opt-in. Setting it resets
  incremental discovery and never revokes already returned descriptors.
- Balanced counts live rows by numeric prefix, chooses quantile boundaries, and
  retains every allocated ID through the captured high watermark. Counts never
  prune membership; concurrent writes affect balance, not disjoint coverage.
- At most 256 count requests, two-second internal probe context, O(partition_count)
  client state. On budget/time exhaustion discard partial cuts and use the
  default. Redis/metadata timeouts and caller cancellation remain separate.
- Fewer rows than requested partitions, unavailable automatic cap, or candidate
  spans exceeding cap*2^20 retain default ranges. Other readiness/count failures
  remain explicit. Require readiness before/after counts; configured caps avoid
  CONFIG probing. No new worker descriptor fields, scheduler or snapshot.
- Preserve partition_count<=1024, schemas, bindings, exact residual evaluation,
  authentication, stale-identity, cancellation and reader lifecycle contracts.
- Compare both strategies and ExecuteQuery with identical process startup/full
  fetch/IPC boundaries, exact source/schema checks and planning included.
