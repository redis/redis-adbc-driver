# Contract

Date: 2026-10-21. Source: merged #414. Author: Codex; requestor: Albert Wong (keep going). Refs [#170](https://github.com/redis/redis-adbc-driver/issues/170). Status: in progress.

Require exactly the three declared loopback TLS endpoints and the same cluster node IDs. Enforce actual aggregate cap 137 on every primary. Create fresh ACL principals on all primaries; update permissions consistently and verify cleanup after partial failures. Existing exact Arrow, denial, stale/malformed descriptor, cancel/release and cross-process assertions remain. Require all 16,384 slots and fixture rows on each of three advertised owners. Add durable Linux architecture coverage without paid resources or production runtime changes. See [intent](intent.md) and [plan](plan.md).
