# Specification

Date: November 13, 2026. Author: Codex. Status: focused validation complete; integration review pending.
[Intent](intent.md), [plan](plan.md).

- A connection retains at most one idle execution pool. Active or streamed SQL
  exclusively leases its pool; overlapping executions allocate independent pools.
- Each lease has a fresh context, cancellation handle, owner registration and
  completion channel. An earlier handle cannot close sockets of a subsequent
  lease. Detachment and socket interruption synchronize through the pool mutex.
- Reuse requires completely consumed replies, an uncanceled/uninterrupted
  execution and no unfinished background work at completion. Read-only direct native errors and missing-value replies are complete; transport/context,
  wrapped, or any uncertain pipeline reply discard the pool after joining
  existing workers and cleanup. Unknown writes/pages are
  never replayed. Timeouts/config identity must match before reuse.
- Connection Close marks closed, drains the idle pool and interrupts/joins active
  executions before shutting maintenance clients. A racing completion cannot
  cache a pool after closure.
- Acquisition skips foreground garbage collection only when a live registered
  compactor already performs it. Disabled/stopped workers retain the existing
  fallback; explicit sweeps and bounded capacity reclamation remain unchanged.
  No catalog/row proof is cached across statement pins.

Acceptance: deterministic transport tests prove one RESP3 handshake for repeated
successful executions, independent overlapping lifetimes, stale cancellation
isolation, no uncertain write replay, timeout replacement, idle cap/Close and
worker-liveness decisions. Existing cancellation/prefetch/Close regressions pass.
An opt-in Software/TLS resource profile records actual dials, client-call counts
and time with complete typed Arrow validation; native before/after measurements
remain the end-to-end performance evidence.

- Unmanaged table loading reuses only the mutable index attributes just checked
  by getTable under the current pin, avoiding one duplicate FT.INFO. Every new
  execution still checks its application definition; external row values and
  index definitions are never cached across statements.

- Failed transactional pipelines and failed/non-acknowledged UNWATCH, DISCARD or
  RESET cleanup cannot reuse a pool even when their errors are complete native
  replies. Queued cleanup is not a cleanup acknowledgment. State is discarded
  by closing the pool, never by replaying a transaction or mutation.
