# Specification

2026-10-30. Base: `9124f0f`. Author: Codex.

- Bound best-effort FT.CURSOR DEL to one second using a fresh cleanup deadline.
  Retain the original coordinator, single attempt, Cancelled/57014 result and
  unknown-write outcome semantics. On failure, server idle expiry is the fallback.
- Fault injection must block both READ and DEL with finite/disabled read timeouts
  for single endpoint and Cluster routing. Verify pool reuse, no READ replay,
  local cursor release, empty owner registrations and zero Arrow allocations.
- Extend the existing verified TLS fixture checker with independent read-only
  principals for both statistics paths, absent Search permission, source-key ACL
  denial and absent SCAN permission. Require complete count values and explicit
  NOPERM; never accept a fabricated zero.
- Validate declared PRIMARY KEY order and CHECK column deduplication, quoted
  column rename and CHECK removal, with absent FOREIGN KEY usage/UNIQUE metadata.
  Duplicate declared primary keys remain allowed under the existing contract.
- Exercise same-schema/cardinality table replacement before retained publication:
  reject it without publishing partial output or leaving payload/quota charges.
  Same-cardinality writes after capture may publish the earlier captured output;
  replay must remain immutable. No source snapshot guarantee is added.
- Test a freshly downloaded published wheel on the actual host platform and
  report its exact version. This does not put the cleanup fix into that release.

See [intent](intent.md), [plan](plan.md), and [validation](validation.md).
