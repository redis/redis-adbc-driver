# Intent: bounded Flex directory predicate candidates

Date: 2026-11-23. Source: `07b24edb`. Status: local candidate, not released.
Requestor: repository user; implementation: Codex agent. Approval: unverified.

Managed Flex scans use exact directories for visibility but discard safe source
Search predicates before fetching projected HASH fields. The TPC-DS failed-query
investigation needs a shared conservative improvement without changing fixtures,
storage, benchmark SQL, or the supported Flex command surface.

Resolve authoritative newest winners and tombstones first, then use bounded
plain Search INKEYS membership to avoid decoding projected fields for nonmatches.
Keep the complete SQL residual and all visibility/ownership/error fences. This
is a candidate optimization, not evidence that any benchmark issue is fixed.
No full benchmark restart, cloud provisioning, or metadata migration is included.

See [specification](spec.md), [plan and evidence](plan.md). Related issues: [TPC-DS Q3 #589](https://github.com/redis/redis-adbc-driver/issues/589), [Q2 #588](https://github.com/redis/redis-adbc-driver/issues/588), [Q4 #590](https://github.com/redis/redis-adbc-driver/issues/590), [Q5 #591](https://github.com/redis/redis-adbc-driver/issues/591), and [Q9 #595](https://github.com/redis/redis-adbc-driver/issues/595).
