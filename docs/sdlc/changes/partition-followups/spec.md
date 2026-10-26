# Specification

2026-10-21; baseline `edbd299`; status: implemented subset and proposed designs.
Requestor: alberttwong. See [intent](intent.md) and [plan](plan.md).

- #358: one consumed/released Arrow parameter row; explicit scalar type allowlist,
  exact typed/base64 parameter encoding with 256 KiB total and 1024 fields/batches.
  No SQL interpolation. Allow arithmetic, CASE, casts and selected deterministic
  functions. Reject global, volatile, user/session-dependent expressions.
- #361: exact row-ID windows no wider than a positive guaranteed aggregate cap;
  one live cursor, no retries or whole-result HASH materialization. Probe fresh
  settings or require explicit deployment lower bound with hidden CONFIG.
  Bound subranges at 2^20 per descriptor; reject larger work rather than truncate.
  Administrators must keep the discovered/configured lower bound valid during
  each open cursor; zero caps and concurrent lowering below it are unsupported.
- #372: disabled by default; enabling returns all immediately discovered ranges
  once, then empty sets with schema. Errors are terminal until reset. No artificial
  asynchronous work, pending sentinel or background goroutine. Standard progress
  options explicitly unsupported; no invented GetInfo capability.
- #359/#360/#362: publish per-operator, layout and consistency designs; keep
  unsupported runtime query/layout classes rejected. Retained results are proposed,
  not shipped; no true Redis snapshot or exactly-once processing is promised.
- #363: retain full upstream configured suite and required native partition tests,
  publish skip reports and reproducible measurements. Evidence is scoped to actual
  environments run; Cloud/Software/macOS packaging require their own results.

Acceptance evidence and outstanding deployment gates are in [plan](plan.md).
Public behavior and resource ownership are in
[partitioned reads](../../../partitioned-reads.md).
