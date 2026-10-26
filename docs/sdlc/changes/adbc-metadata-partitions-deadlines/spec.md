# Specification

2026-10-22. Author: Codex; requestor: Albert Wong. Base: `6508626`.
Status: implemented; review pending. Issues linked in [intent](intent.md).

- Implement ConnectionGetStatistics through the existing C shim/driverbase path.
  Use upstream canonical schemas without locally redefining them. Custom names
  are empty. Approximate requests emit standard int64 table row counts for
  permanent managed tables only; no view/temporary/application/foreign counts.
  Search readiness is required and permission errors remain errors. Exact
  requests fail NOT_IMPLEMENTED. Filters and empty nested lists are documented
  in [statistics](../../../adbc-statistics.md). Unsupported quantities are absent.
- Admit SUBSTR/LEFT/RIGHT in the retained stable-function allowlist. Ordinary
  semantics and cumulative admission remain authoritative. Pattern SUBSTRING,
  temporary sources and every previously excluded operator remain excluded.
  Compare complete typed Arrow output; preserve global execution before split.
  Publish [coverage and snapshot assessment](../../../partition-coverage.md).
- Force go-redis ContextTimeoutEnabled for ordinary single/Cluster clients.
  Normalize context-driven network deadline errors, including timer races,
  before producing read-timeout hints. Preserve TIMEOUT/57014 versus
  CANCELLED/57014 and ordinary socket-timeout IO errors. No retry policy or
  write rollback behavior changes. Explicit cancellation still waits for an
  already-sent command when no deadline interrupts it; no server termination
  guarantee. Existing retained source socket cancellation remains separate.

Acceptance: canonical schemas and values through Go and native driver manager,
filter/missing/empty/exact cases, read-only ACL denial where available, retained
value equivalence, blocked command/pipeline deadlines under RESP2/3 with finite
and disabled read timeouts, pool reuse and no observed replay. Full Go suites
use verified Redis Software 4x QPF. Cluster/TLS deployment testing and published
wheels require separate evidence; do not infer them from local proxy tests.

See [plan](plan.md), [evidence](evidence.md).
