# Specification: capability tracking and Redis Software matrix

Date: 2026-10-22. Author: Codex. Status: implemented.

- Link each of the 20 investigated Arrow matrix entries to a GitHub issue,
  including companion-service entries subsequently classified as N/A.
  New issues must include existing repository labels at creation, current
  source evidence, scoped work and concrete acceptance/validation criteria.
- Reuse transaction issue #269 and CDC issue #457 with investigation comments;
  create focused follow-ups for the other 18 entries. Distinguish normalization
  of physical Arrow layouts from numerical corruption or unsupported SQL.
- Add a Redis Software matrix to README and its contents. Include verified 4x
  QPF, RESP3/RESP2, Search/HASHes, topology, transport/authentication, RBAC,
  availability/durability and unsupported server APIs/features.
- Use green for supported driver paths, yellow for restricted driver behavior,
  red for unsupported driver capabilities/compatibility, and N/A for server
  administration, companion services or external execution engines.
  Active-Active is explicitly
  unsupported/unvalidated by this driver even though Redis supports Search there;
  do not claim it is automatically refused or unavailable in the server.
- Cite official Redis documentation for server behavior and local source/guides
  for driver behavior. No fresh runtime compatibility or performance claims.

No implementation changes, deployment, new Redis benchmarks or configuration
changes are in scope. See the [intent](intent.md) and [verification](plan.md).
